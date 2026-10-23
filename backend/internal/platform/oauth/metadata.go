package oauth

// ProtectedResourceMetadata is RFC 9728 metadata for the MCP endpoint.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceName           string   `json:"resource_name"`
}

// AuthorizationServerMetadata is RFC 8414 metadata.
type AuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported"`
	AuthorizationResponseISSSupported bool     `json:"authorization_response_iss_parameter_supported"`
}

func (s *Service) ProtectedResourceMetadata() ProtectedResourceMetadata {
	return ProtectedResourceMetadata{
		Resource: s.resource, AuthorizationServers: []string{s.base}, ScopesSupported: []string{Scope},
		BearerMethodsSupported: []string{"header"}, ResourceName: "创意空间",
	}
}

func (s *Service) AuthorizationServerMetadata() AuthorizationServerMetadata {
	return AuthorizationServerMetadata{
		Issuer: s.base, AuthorizationEndpoint: s.base + "/oauth/authorize", TokenEndpoint: s.base + "/oauth/token",
		RegistrationEndpoint:   s.base + "/oauth/register",
		ResponseTypesSupported: []string{"code"}, GrantTypesSupported: []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported: []string{"S256"}, TokenEndpointAuthMethodsSupported: authMethods,
		ScopesSupported: []string{Scope}, ClientIDMetadataDocumentSupported: true, AuthorizationResponseISSSupported: true,
	}
}
