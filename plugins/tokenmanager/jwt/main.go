package main

import (
	"context"
	"time"

	"github.com/cernbox/gohub/goconfig"

	"github.com/cernbox/reva/api"
	"github.com/dgrijalva/jwt-go"
	"github.com/grpc-ecosystem/go-grpc-middleware/tags/zap"
	"go.uber.org/zap"
)

func RegisterConfig(gc *goconfig.GoConfig) {
	gc.Add("tokenmanager-plugin-sign-secret", "foo", "the secret to sign tokens.")
}

func New(gc *goconfig.GoConfig, logger *zap.Logger) (api.TokenManager, error) {
	return &tokenManager{
		signSecret: gc.GetString("tokenmanager-plugin-sign-secret"),
		logger:     logger}, nil
}

type tokenManager struct {
	signSecret string
	logger     *zap.Logger
}

func (tm *tokenManager) ForgeToken(ctx context.Context, user *api.User) (string, error) {
	l := ctx_zap.Extract(ctx)
	token := jwt.New(jwt.GetSigningMethod("HS256"))
	claims := token.Claims.(jwt.MapClaims)
	claims["account_id"] = user.AccountId
	claims["groups"] = user.Groups
	claims["exp"] = time.Now().Add(time.Second * time.Duration(3600))
	tokenString, err := token.SignedString([]byte(tm.signSecret))
	if err != nil {
		l.Error("", zap.Error(err))
		return "", err
	}
	return tokenString, nil
}

func (tm *tokenManager) VerifyToken(ctx context.Context, token string) (*api.User, error) {
	l := ctx_zap.Extract(ctx)
	rawToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		return []byte(tm.signSecret), nil
	})
	if err != nil {
		l.Error("invalid token", zap.Error(err), zap.String("token", token))
		return nil, err
	}
	if !rawToken.Valid {
		l.Error("invalid token", zap.Error(err), zap.String("token", token))
		return nil, err

	}

	claims := rawToken.Claims.(jwt.MapClaims)
	user := &api.User{
		AccountId: claims["account_id"].(string),
	}
	return user, nil
}
