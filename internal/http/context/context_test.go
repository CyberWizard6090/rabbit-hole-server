package contextutil

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetUserIDReturnsStoredUint(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set("userID", uint(42))
	got, err := GetUserID(c)
	if err != nil || got != 42 {
		t.Fatalf("got=%d err=%v", got, err)
	}
}

func TestGetUserIDRejectsMissingOrWrongType(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	if _, err := GetUserID(c); err == nil {
		t.Fatal("expected missing user error")
	}
	c.Set("userID", uint64(42))
	if _, err := GetUserID(c); err == nil {
		t.Fatal("expected wrong type error")
	}
}
