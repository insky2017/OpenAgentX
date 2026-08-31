package remotehttps

import (
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
)

// CertificatePrincipalResolver derives the authenticated principal from the
// verified client certificate and limits that principal to an explicit set of
// logical Agents. The binding is checked at registration; subsequent requests
// remain protected by the Worker Session Token and lease/fencing guards.
type CertificatePrincipalResolver struct {
	bindings map[string]map[string]struct{}
}

func NewCertificatePrincipalResolver(bindings map[string][]string) (*CertificatePrincipalResolver, error) {
	if len(bindings) == 0 {
		return nil, fmt.Errorf("at least one mTLS principal binding is required")
	}
	result := make(map[string]map[string]struct{}, len(bindings))
	for principal, agents := range bindings {
		if err := domain.ValidateOpaqueID("mTLS principal", principal); err != nil {
			return nil, err
		}
		if len(agents) == 0 {
			return nil, fmt.Errorf("mTLS principal %q has no Agent bindings", principal)
		}
		set := make(map[string]struct{}, len(agents))
		for _, agent := range agents {
			if err := domain.ValidateIdentifier("agent_id", agent); err != nil {
				return nil, err
			}
			set[agent] = struct{}{}
		}
		result[principal] = set
	}
	return &CertificatePrincipalResolver{bindings: result}, nil
}

func (r *CertificatePrincipalResolver) ResolvePrincipal(request *http.Request) (string, error) {
	if r == nil || request == nil || request.TLS == nil || len(request.TLS.PeerCertificates) == 0 || len(request.TLS.VerifiedChains) == 0 {
		return "", domain.ErrUnauthorized
	}
	principal, err := CertificatePrincipal(request.TLS.PeerCertificates[0])
	if err != nil {
		return "", err
	}
	if _, ok := r.bindings[principal]; !ok {
		return "", domain.ErrUnauthorized
	}
	return principal, nil
}

func (r *CertificatePrincipalResolver) ValidateAgent(request *http.Request, agentID string) error {
	principal, err := r.ResolvePrincipal(request)
	if err != nil {
		return domain.ErrUnauthorized
	}
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		return err
	}
	if _, ok := r.bindings[principal][agentID]; !ok {
		return domain.ErrForbidden("mTLS principal is not bound to requested Agent")
	}
	return nil
}

func (r *CertificatePrincipalResolver) ValidateRegistration(request *http.Request, registration openapi.RegisterRequest) error {
	if registration.Transport != domain.WorkerTransportHTTPS {
		return domain.ErrForbidden("mTLS Worker registration must use https transport")
	}
	return r.ValidateAgent(request, registration.AgentID)
}

// CertificatePrincipal uses a URI SAN when present (the preferred explicit
// identity form), otherwise the certificate CommonName for simple deployments.
func CertificatePrincipal(certificate *x509.Certificate) (string, error) {
	if certificate == nil {
		return "", domain.ErrUnauthorized
	}
	if len(certificate.URIs) > 1 {
		return "", domain.ErrUnauthorized
	}
	principal := ""
	if len(certificate.URIs) == 1 {
		principal = certificate.URIs[0].String()
	} else {
		principal = certificate.Subject.CommonName
	}
	principal = strings.TrimSpace(principal)
	if err := domain.ValidateOpaqueID("mTLS principal", principal); err != nil {
		return "", domain.ErrUnauthorized
	}
	return principal, nil
}
