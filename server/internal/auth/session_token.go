package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

var ErrNotSessionToken = errors.New("auth: token is not a session token")

func ParseSessionHS256(tokenString string) (*jwt.Token, error) {
	token, err := ParseHS256(tokenString)
	if err != nil {
		return nil, err
	}
	if err := requireNoTypeClaim(token); err != nil {
		return nil, err
	}
	return token, nil
}

func requireNoTypeClaim(token *jwt.Token) error {
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return jwt.ErrTokenInvalidClaims
	}
	if typ, _ := claims["typ"].(string); typ != "" {
		return ErrNotSessionToken
	}
	return nil
}
