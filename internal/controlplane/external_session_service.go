package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"openagentx/internal/domain"
)

type ExternalSessionState interface {
	BindExternalSession(context.Context, string, domain.ExternalSessionBinding, int64) (*domain.ExternalSessionBinding, error)
	RevokeExternalSession(context.Context, string, string, int64) error
	GetExternalSessionForOwner(context.Context, string, string) (*domain.ExternalSessionBinding, error)
	AuthenticateExternalSession(context.Context, string) (*domain.ExternalSessionBinding, error)
	SendExternalMessage(context.Context, string, domain.ExternalMessage) (*domain.ExternalMessage, error)
	ListExternalInbox(context.Context, string, int64, int) ([]domain.ExternalMessage, error)
	GetExternalMessage(context.Context, string, string) (*domain.ExternalMessage, error)
	AcknowledgeExternalMessage(context.Context, string, string) (*domain.ExternalMessage, error)
}

type ExternalSessionService struct {
	state  ExternalSessionState
	broker WakeupBroker
	now    func() time.Time
}

func NewExternalSessionService(state ExternalSessionState, broker WakeupBroker, now func() time.Time) (*ExternalSessionService, error) {
	if state == nil {
		return nil, fmt.Errorf("external session state is required")
	}
	if broker == nil {
		broker = NewMemoryWakeupBroker()
	}
	if now == nil {
		now = time.Now
	}
	return &ExternalSessionService{state, broker, now}, nil
}

// Bind returns the new secret only here. It must never be logged or projected
// as binding metadata. Repeating a rotation with a stale generation fails.
func (s *ExternalSessionService) Bind(ctx context.Context, owner string, input domain.BindExternalSessionInput) (*domain.ExternalSessionBinding, string, error) {
	if err := input.Validate(); err != nil {
		return nil, "", err
	}
	if err := domain.ValidateOpaqueID("owner_principal", owner); err != nil {
		return nil, "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	secret := "oax_ext_" + base64.RawURLEncoding.EncodeToString(raw)
	peers := append([]string(nil), input.AllowedPeerAgentIDs...)
	sort.Strings(peers)
	now := s.now().UTC()
	b := domain.ExternalSessionBinding{ID: "external-" + uuid.NewString(), AgentID: input.AgentID, HostID: input.HostID, ThreadID: input.ThreadID, Generation: input.ExpectedGeneration + 1, State: "active", AllowedPeerAgentIDs: peers, TokenDigest: externalTokenDigest(secret), TokenExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now, UpdatedAt: now}
	bound, err := s.state.BindExternalSession(ctx, owner, b, input.ExpectedGeneration)
	if err != nil {
		return nil, "", err
	}
	return bound, secret, nil
}
func (s *ExternalSessionService) Revoke(ctx context.Context, owner, agentID string, generation int64) error {
	return s.state.RevokeExternalSession(ctx, owner, agentID, generation)
}
func (s *ExternalSessionService) Binding(ctx context.Context, owner, agentID string) (*domain.ExternalSessionBinding, error) {
	return s.state.GetExternalSessionForOwner(ctx, owner, agentID)
}
func (s *ExternalSessionService) Status(ctx context.Context, token string) (*domain.ExternalSessionBinding, error) {
	return s.state.AuthenticateExternalSession(ctx, externalTokenDigest(token))
}
func (s *ExternalSessionService) Send(ctx context.Context, token string, input domain.SendExternalMessageInput) (*domain.ExternalMessage, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	m := domain.ExternalMessage{ID: "external-message-" + uuid.NewString(), TargetAgentID: input.TargetAgentID, Kind: input.Kind, ReplyToMessageID: input.ReplyToMessageID, Content: input.Content, IdempotencyKey: input.IdempotencyKey, OriginTaskID: input.OriginTaskID, PayloadDigest: hex.EncodeToString(digest[:]), DeliveryState: "pending", CreatedAt: s.now().UTC()}
	result, err := s.state.SendExternalMessage(ctx, externalTokenDigest(token), m)
	if err != nil {
		return nil, err
	}
	s.broker.Publish("external-inbox:" + result.TargetAgentID)
	return result, nil
}
func (s *ExternalSessionService) Inbox(ctx context.Context, token string, after int64, limit int) ([]domain.ExternalMessage, error) {
	if after < 0 {
		return nil, domain.ErrInvalidInput("after sequence must be nonnegative")
	}
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 200 {
		return nil, domain.ErrInvalidInput("limit must be between 1 and 200")
	}
	return s.state.ListExternalInbox(ctx, externalTokenDigest(token), after, limit)
}
func (s *ExternalSessionService) Get(ctx context.Context, token, id string) (*domain.ExternalMessage, error) {
	if err := domain.ValidateOpaqueID("message_id", id); err != nil {
		return nil, err
	}
	return s.state.GetExternalMessage(ctx, externalTokenDigest(token), id)
}
func (s *ExternalSessionService) Ack(ctx context.Context, token, id string) (*domain.ExternalMessage, error) {
	if err := domain.ValidateOpaqueID("message_id", id); err != nil {
		return nil, err
	}
	return s.state.AcknowledgeExternalMessage(ctx, externalTokenDigest(token), id)
}
func externalTokenDigest(token string) string {
	if !strings.HasPrefix(token, "oax_ext_") || len(token) != 51 {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
