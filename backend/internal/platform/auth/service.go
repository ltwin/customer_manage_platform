package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Account 是账号的领域形态（/me 返回面）；永不携带 password_hash。
type Account struct {
	ID        string
	CreatedAt time.Time
}

// ErrInvalidPassword：登录密码不匹配（对外 401 unauthorized）。
var ErrInvalidPassword = errors.New("密码错误")

// AccountReader 是认证所需的最小仓储面（接口定义在消费方）。
type AccountReader interface {
	// FirstAccount 返回唯一账号的认证信息（首版单账号，§4.1）。
	FirstAccount(ctx context.Context) (id, passwordHash string, err error)
	AccountByID(ctx context.Context, id string) (Account, error)
}

// Service 承载登录与账号读取的领域编排；不 import 路由框架（ADR-003）。
type Service struct {
	accounts AccountReader
	tokens   *TokenIssuer
}

// NewService 组装认证服务。
func NewService(accounts AccountReader, tokens *TokenIssuer) *Service {
	return &Service{accounts: accounts, tokens: tokens}
}

// Login 校验密码（bcrypt）并签发 token；密码不匹配返回 ErrInvalidPassword。
func (s *Service) Login(ctx context.Context, password string) (string, error) {
	id, hash, err := s.accounts.FirstAccount(ctx)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	if !VerifyPassword(hash, password) {
		return "", ErrInvalidPassword
	}
	token, err := s.tokens.Issue(id)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	return token, nil
}

// AccountByID 读取账号信息（/me）。
func (s *Service) AccountByID(ctx context.Context, id string) (Account, error) {
	acct, err := s.accounts.AccountByID(ctx, id)
	if err != nil {
		return Account{}, fmt.Errorf("read account: %w", err)
	}
	return acct, nil
}

// ParseToken 解析 Bearer token 归属账号（auth 中间件用）。
func (s *Service) ParseToken(token string) (string, error) {
	return s.tokens.Parse(token)
}
