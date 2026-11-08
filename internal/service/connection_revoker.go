package service

type ConnectionRevoker interface {
	DisconnectUser(userID uint) int
	DisconnectUserFeeds(userID uint) int
}

func disconnectUser(r ConnectionRevoker, userID uint) {
	if r != nil {
		r.DisconnectUser(userID)
	}
}

func disconnectUserFeeds(r ConnectionRevoker, userID uint) {
	if r != nil {
		r.DisconnectUserFeeds(userID)
	}
}
