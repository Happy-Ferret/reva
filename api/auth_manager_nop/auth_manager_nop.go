package auth_manager_nop

import (
	"context"
	//"crypto/tls"
	//"fmt"

	"github.com/cernbox/reva/api"
	//"gopkg.in/ldap.v2"
)

type authManager struct {
}

func New() api.AuthManager {
	return &authManager{}
}

func (am *authManager) Authenticate(ctx context.Context, clientID, clientSecret string) (*api.User, error) {
	return &api.User{AccountId: clientID, Groups: []string{}}, nil
}
