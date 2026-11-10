package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"strings"
	"time"

	"rabbit-hole-server/internal/domain"
)

// ErrSessionNotFound is returned when a user asks to end a session that is not theirs or no longer exists.
var ErrSessionNotFound = errors.New("session not found")

// SecurityNotifier tells the owner of an account about events they would want to know about.
// Failures are logged by the implementation and never fail the request.
type SecurityNotifier interface {
	NotifyNewSignIn(ctx context.Context, user *domain.User, device, maskedIP string, at time.Time)
	NotifyPasswordChanged(ctx context.Context, user *domain.User)
}

func (s *AuthService) WithSecurityNotifier(n SecurityNotifier) *AuthService {
	s.security = n
	return s
}

// ClientInfo describes the client of a request; sessions remember it so the owner can recognize them.
type ClientInfo struct {
	IP        string
	UserAgent string
}

type clientInfoKey struct{}

func WithClientInfo(ctx context.Context, ip, userAgent string) context.Context {
	if len(userAgent) > 255 {
		userAgent = userAgent[:255]
	}
	return context.WithValue(ctx, clientInfoKey{}, ClientInfo{IP: ip, UserAgent: userAgent})
}

func clientInfoFrom(ctx context.Context) ClientInfo {
	info, _ := ctx.Value(clientInfoKey{}).(ClientInfo)
	return info
}

// ListSessions returns the devices the user is signed in on; currentSession marks the caller's own.
func (s *AuthService) ListSessions(ctx context.Context, userID uint, currentSession string) ([]domain.SessionInfo, error) {
	sessions, err := s.repo.ListSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		sessions[i].Device = DeviceLabel(sessions[i].UserAgent)
		sessions[i].Current = currentSession != "" && sessions[i].ID == currentSession
	}
	return sessions, nil
}

// RevokeSession ends one device. The access token of that device stops working at once.
func (s *AuthService) RevokeSession(ctx context.Context, userID uint, sessionID string) error {
	ended, err := s.repo.RevokeUserSession(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	if !ended {
		return ErrSessionNotFound
	}
	disconnectUser(s.revoker, userID)
	return nil
}

// RevokeOtherSessions signs the user out everywhere except the device they are using.
func (s *AuthService) RevokeOtherSessions(ctx context.Context, userID uint, currentSession string) (int, error) {
	ended, err := s.repo.RevokeOtherSessions(ctx, userID, currentSession)
	if err != nil {
		return 0, err
	}
	if ended > 0 {
		disconnectUser(s.revoker, userID)
	}
	return ended, nil
}

// announceSignIn remembers the device of a sign-in and mails the owner when it is one they have not used
// before. The very first device of an account is the baseline and is not announced.
func (s *AuthService) announceSignIn(ctx context.Context, user *domain.User) {
	client := clientInfoFrom(ctx)
	device := DeviceLabel(client.UserAgent)
	sum := sha256.Sum256([]byte(device))
	isNew, hadOthers, err := s.repo.TouchLoginDevice(context.WithoutCancel(ctx), user.ID, hex.EncodeToString(sum[:]), device)
	if err != nil {
		slog.Warn("auth: cannot remember the sign-in device", "user_id", user.ID, "error", err)
		return
	}
	if isNew && hadOthers && s.security != nil {
		s.security.NotifyNewSignIn(ctx, user, device, MaskIP(client.IP), time.Now())
	}
}

// DeviceLabel turns a User-Agent into "Browser on System" for people and for telling devices apart.
func DeviceLabel(userAgent string) string {
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		return "Unknown device"
	}
	lower := strings.ToLower(ua)
	system := ""
	switch {
	case strings.Contains(lower, "android"):
		system = "Android"
	case strings.Contains(lower, "iphone"), strings.Contains(lower, "ipad"), strings.Contains(lower, "ipod"):
		system = "iOS"
	case strings.Contains(lower, "windows"):
		system = "Windows"
	case strings.Contains(lower, "mac os x"), strings.Contains(lower, "macintosh"):
		system = "macOS"
	case strings.Contains(lower, "cros"):
		system = "ChromeOS"
	case strings.Contains(lower, "linux"), strings.Contains(lower, "x11"):
		system = "Linux"
	}
	browser := ""
	switch {
	case strings.Contains(lower, "edg/"), strings.Contains(lower, "edga/"), strings.Contains(lower, "edgios/"):
		browser = "Edge"
	case strings.Contains(lower, "opr/"), strings.Contains(lower, "opera"):
		browser = "Opera"
	case strings.Contains(lower, "firefox/"), strings.Contains(lower, "fxios/"):
		browser = "Firefox"
	case strings.Contains(lower, "chrome/"), strings.Contains(lower, "crios/"):
		browser = "Chrome"
	case strings.Contains(lower, "safari/"):
		browser = "Safari"
	}
	switch {
	case browser != "" && system != "":
		return browser + " on " + system
	case browser != "":
		return browser
	case system != "":
		return system
	}
	name := ua
	if i := strings.IndexAny(name, "/ "); i > 0 {
		name = name[:i]
	}
	if len(name) > 40 {
		name = name[:40]
	}
	return name
}

// MaskIP hides the host part of an address: enough to recognize a network, not to locate a person.
func MaskIP(ip string) string {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return ""
	}
	if v4 := parsed.To4(); v4 != nil {
		return strings.Join(strings.Split(v4.String(), ".")[:3], ".") + ".x"
	}
	return parsed.Mask(net.CIDRMask(48, 128)).String() + "/48"
}
