package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/ids"
	"local-agent/internal/security"
)

// Manager stores MCP server configs, transports, cached tool schemas, and local policy overrides.
type Manager struct {
	mu            sync.RWMutex
	servers       map[string]core.MCPServer
	policies      map[string]core.MCPToolPolicy
	tools         map[string]map[string]MCPToolSchema
	lastRefresh   map[string]time.Time
	transports    map[string]MCPTransport
	factory       TransportFactory
	defaultTimout int
	network       config.NetworkPolicy
}

// Option customizes the MCP manager.
type Option func(*Manager)

// WithTransportFactory replaces the runtime transport factory.
func WithTransportFactory(factory TransportFactory) Option {
	return func(m *Manager) {
		if factory != nil {
			m.factory = factory
		}
	}
}

// WithDefaultTimeoutSeconds changes the per-server timeout fallback.
func WithDefaultTimeoutSeconds(seconds int) Option {
	return func(m *Manager) {
		if seconds > 0 {
			m.defaultTimout = seconds
		}
	}
}

// NewManager creates an MCP manager.
func NewManager(options ...Option) *Manager {
	m := &Manager{
		servers:       map[string]core.MCPServer{},
		policies:      map[string]core.MCPToolPolicy{},
		tools:         map[string]map[string]MCPToolSchema{},
		lastRefresh:   map[string]time.Time{},
		transports:    map[string]MCPTransport{},
		factory:       DefaultTransportFactory{},
		defaultTimout: 30,
		network:       config.Default().Policy.Network,
	}
	for _, option := range options {
		option(m)
	}
	return m
}

// SetNetworkPolicy sets outbound HTTP MCP endpoint constraints.
func (m *Manager) SetNetworkPolicy(policy config.NetworkPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.network = policy
}

// LoadConfig loads server and policy configuration files.
func (m *Manager) LoadConfig(serverPath, policyPath string) error {
	if err := m.LoadServers(serverPath); err != nil {
		return err
	}
	if err := m.LoadToolPolicies(policyPath); err != nil {
		return err
	}
	return nil
}

// LoadServers loads config/mcp.servers.yaml.
func (m *Manager) LoadServers(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("load MCP servers config %s: %w", path, err)
	}
	var file ServersFile
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), &file); err != nil {
		return fmt.Errorf("parse MCP servers config %s: %w", path, err)
	}

	next := make(map[string]core.MCPServer, len(file.Servers))
	for _, input := range file.Servers {
		item, err := normalizeServer(input, m.defaultTimout, false)
		if err != nil {
			return err
		}
		if _, exists := next[item.ID]; exists {
			return fmt.Errorf("duplicate MCP server id: %s", item.ID)
		}
		next[item.ID] = item
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers = next
	m.transports = map[string]MCPTransport{}
	m.tools = map[string]map[string]MCPToolSchema{}
	m.lastRefresh = map[string]time.Time{}
	return nil
}

// LoadToolPolicies loads config/mcp.tool-policies.yaml.
func (m *Manager) LoadToolPolicies(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("load MCP tool policies config %s: %w", path, err)
	}
	var file ToolPoliciesFile
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), &file); err != nil {
		return fmt.Errorf("parse MCP tool policies config %s: %w", path, err)
	}

	next := map[string]core.MCPToolPolicy{}
	for id, input := range file.Tools {
		policy, err := normalizePolicy(id, input)
		if err != nil {
			return err
		}
		next[policy.ID] = policy
	}
	for _, input := range file.ToolPolicies {
		policy, err := normalizePolicy(input.ID, input)
		if err != nil {
			return err
		}
		next[policy.ID] = policy
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies = next
	return nil
}

// ListServers returns configured MCP servers with secret-bearing fields redacted.
func (m *Manager) ListServers() []core.MCPServer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]core.MCPServer, 0, len(m.servers))
	for _, server := range m.servers {
		items = append(items, sanitizeServer(server))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

// GetServer returns a configured server by ID with secret-bearing fields redacted.
func (m *Manager) GetServer(id string) (core.MCPServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	server, ok := m.servers[id]
	if !ok {
		return core.MCPServer{}, fmt.Errorf("mcp server not found: %s", id)
	}
	return sanitizeServer(server), nil
}

// CreateServer registers a new MCP server.
func (m *Manager) CreateServer(input ServerInput) (core.MCPServer, error) {
	item, err := normalizeServer(input, m.defaultTimout, true)
	if err != nil {
		return core.MCPServer{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers[item.ID] = item
	delete(m.transports, item.ID)
	delete(m.tools, item.ID)
	delete(m.lastRefresh, item.ID)
	return sanitizeServer(item), nil
}

// UpdateServer mutates a server config.
func (m *Manager) UpdateServer(id string, input ServerInput) (core.MCPServer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.servers[id]
	if !ok {
		return core.MCPServer{}, fmt.Errorf("mcp server not found: %s", id)
	}
	merged := serverToInput(item)
	if input.Name != "" {
		merged.Name = input.Name
	}
	if input.Transport != "" {
		merged.Transport = input.Transport
	}
	if input.Dialect != "" {
		merged.Dialect = input.Dialect
	}
	if !compatibilityInputEmpty(input.Compatibility) {
		merged.Compatibility = input.Compatibility
	}
	if input.Command != "" {
		merged.Command = input.Command
	}
	if input.Args != nil {
		merged.Args = input.Args
	}
	if input.Cwd != "" {
		merged.Cwd = input.Cwd
	}
	if input.URL != "" {
		merged.URL = input.URL
	}
	if input.MessageURL != "" {
		merged.MessageURL = input.MessageURL
	}
	if input.Headers != nil {
		merged.Headers = input.Headers
	}
	if input.Enabled != nil {
		merged.Enabled = input.Enabled
	}
	if input.Env != nil {
		merged.Env = input.Env
	}
	if input.TimeoutSeconds > 0 {
		merged.TimeoutSeconds = input.TimeoutSeconds
	}
	updated, err := normalizeServer(merged, m.defaultTimout, false)
	if err != nil {
		return core.MCPServer{}, err
	}
	updated.ID = id
	updated.CreatedAt = item.CreatedAt
	updated.UpdatedAt = time.Now().UTC()
	m.servers[id] = updated
	delete(m.transports, id)
	delete(m.tools, id)
	delete(m.lastRefresh, id)
	return sanitizeServer(updated), nil
}

// RuntimeState returns cached tool schema state for one server.
func (m *Manager) RuntimeState(id string) (RuntimeState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.servers[id]; !ok {
		return RuntimeState{}, fmt.Errorf("mcp server not found: %s", id)
	}
	state := RuntimeState{ServerID: id}
	if refreshed, ok := m.lastRefresh[id]; ok {
		value := refreshed
		state.LastRefreshAt = &value
	}
	if schemas := m.tools[id]; len(schemas) > 0 {
		state.Tools = sortedSchemas(schemas)
		state.ToolCount = len(state.Tools)
	}
	return state, nil
}

// ListToolPolicies returns local policy overrides.
func (m *Manager) ListToolPolicies() []core.MCPToolPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]core.MCPToolPolicy, 0, len(m.policies))
	for _, policy := range m.policies {
		policy.Effects = append([]string(nil), policy.Effects...)
		items = append(items, policy)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

// UpdateToolPolicy upserts a legacy boolean policy override.
func (m *Manager) UpdateToolPolicy(id string, requiresApproval bool, riskLevel, reason string) core.MCPToolPolicy {
	input := ToolPolicyInput{
		Effects:          nil,
		RequiresApproval: &requiresApproval,
		RiskLevel:        riskLevel,
		Reason:           reason,
	}
	policy, _ := m.UpdateToolPolicyInput(id, input)
	return policy
}

// UpdateToolPolicyInput upserts a full tool policy override.
func (m *Manager) UpdateToolPolicyInput(id string, input ToolPolicyInput) (core.MCPToolPolicy, error) {
	policy, err := normalizePolicy(id, input)
	if err != nil {
		return core.MCPToolPolicy{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies[policy.ID] = policy
	return policy, nil
}

// PolicyProfile resolves the effective policy profile for a concrete server tool.
func (m *Manager) PolicyProfile(serverID, toolName string) (core.MCPPolicyProfile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	server, ok := m.servers[serverID]
	if !ok {
		return core.MCPPolicyProfile{}, fmt.Errorf("mcp server not found: %s", serverID)
	}
	if !server.Enabled {
		return core.MCPPolicyProfile{}, fmt.Errorf("mcp server is disabled: %s", serverID)
	}

	key := PolicyKey(serverID, toolName)
	if policy, ok := m.policies[key]; ok {
		return profileFromPolicy(serverID, toolName, policy), nil
	}
	if policy, ok := m.policies[toolName]; ok {
		return profileFromPolicy(serverID, toolName, policy), nil
	}

	if schemas := m.tools[serverID]; schemas != nil {
		if schema, ok := schemas[toolName]; ok {
			return profileFromSchema(serverID, toolName, schema), nil
		}
	}

	return core.MCPPolicyProfile{
		ServerID:         serverID,
		ToolName:         toolName,
		Effects:          []string{"unknown.effect"},
		Approval:         ApprovalRequire,
		RequiresApproval: true,
		RiskLevel:        "unknown",
		Reason:           "MCP tool is not known locally",
		Known:            false,
		Enabled:          true,
		Confidence:       0.4,
	}, nil
}

// RefreshTools lists tools from a server and caches the schemas.
func (m *Manager) RefreshTools(ctx context.Context, serverID string) ([]MCPToolSchema, error) {
	tools, err := m.ListTools(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return tools, nil
}

// ListTools lists tools from the selected server and updates the schema cache.
func (m *Manager) ListTools(ctx context.Context, serverID string) ([]MCPToolSchema, error) {
	transport, err := m.transportFor(ctx, serverID)
	if err != nil {
		return nil, err
	}
	tools, err := transport.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MCP tools for server %s: %w", serverID, err)
	}

	index := map[string]MCPToolSchema{}
	for _, schema := range tools {
		if strings.TrimSpace(schema.Name) == "" {
			continue
		}
		schema.InputSchema = cloneMap(schema.InputSchema)
		schema.Metadata = cloneMap(schema.Metadata)
		index[schema.Name] = schema
	}
	now := time.Now().UTC()

	m.mu.Lock()
	m.tools[serverID] = index
	m.lastRefresh[serverID] = now
	m.mu.Unlock()

	return sortedSchemas(index), nil
}

// CallTool calls a concrete MCP server tool.
func (m *Manager) CallTool(ctx context.Context, serverID string, toolName string, input map[string]any) (*MCPToolResult, error) {
	transport, err := m.transportFor(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(toolName) == "" {
		return nil, fmt.Errorf("mcp tool_name is required")
	}
	result, err := transport.CallTool(ctx, toolName, cloneMap(input))
	if err != nil {
		return nil, fmt.Errorf("call MCP tool %s on server %s: %w", toolName, serverID, err)
	}
	if result == nil {
		return &MCPToolResult{}, nil
	}
	return result, nil
}

// Health checks that a server config is callable and the transport starts.
func (m *Manager) Health(ctx context.Context, serverID string) error {
	transport, err := m.transportFor(ctx, serverID)
	if err != nil {
		return err
	}
	if health, ok := transport.(transportHealth); ok {
		return health.Health(ctx)
	}
	return nil
}

func (m *Manager) transportFor(ctx context.Context, serverID string) (MCPTransport, error) {
	server, err := m.privateServer(serverID)
	if err != nil {
		return nil, err
	}
	if !server.Enabled {
		return nil, fmt.Errorf("mcp server is disabled: %s", serverID)
	}

	m.mu.RLock()
	transport := m.transports[serverID]
	m.mu.RUnlock()
	if transport != nil {
		return transport, nil
	}
	if server.Transport == TransportHTTP || server.Transport == TransportSSE {
		m.mu.RLock()
		network := m.network
		m.mu.RUnlock()
		decision := security.ValidateNetworkURL(network, server.URL, http.MethodPost, server.Compatibility.MaxPayloadBytes)
		if !decision.Allowed {
			return nil, fmt.Errorf("network policy denied MCP HTTP endpoint: %s", decision.Reason)
		}
		if server.MessageURL != "" {
			decision = security.ValidateNetworkURL(network, server.MessageURL, http.MethodPost, server.Compatibility.MaxPayloadBytes)
			if !decision.Allowed {
				return nil, fmt.Errorf("network policy denied MCP HTTP message endpoint: %s", decision.Reason)
			}
		}
	}

	cfg := TransportConfig{
		ServerID:       server.ID,
		Transport:      server.Transport,
		Dialect:        server.Dialect,
		Compatibility:  server.Compatibility,
		Command:        server.Command,
		Args:           append([]string(nil), server.Args...),
		Cwd:            server.Cwd,
		URL:            server.URL,
		MessageURL:     server.MessageURL,
		Headers:        copyStringMap(server.Headers),
		Env:            copyStringMap(server.Environment),
		TimeoutSeconds: server.TimeoutSeconds,
	}
	transport, err = m.factory.NewTransport(cfg)
	if err != nil {
		return nil, err
	}
	if err := transport.Start(ctx); err != nil {
		_ = transport.Close(context.Background())
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.transports[serverID]; existing != nil {
		_ = transport.Close(context.Background())
		return existing, nil
	}
	m.transports[serverID] = transport
	return transport, nil
}

func (m *Manager) privateServer(id string) (core.MCPServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	server, ok := m.servers[id]
	if !ok {
		return core.MCPServer{}, fmt.Errorf("mcp server not found: %s", id)
	}
	return server, nil
}

// PolicyKey returns the stable local override key for a server tool.
func PolicyKey(serverID, toolName string) string {
	return "mcp." + strings.TrimSpace(serverID) + "." + strings.TrimSpace(toolName)
}

func normalizeServer(input ServerInput, defaultTimeout int, allowGeneratedID bool) (core.MCPServer, error) {
	id := strings.TrimSpace(os.ExpandEnv(input.ID))
	name := strings.TrimSpace(os.ExpandEnv(input.Name))
	if id == "" && allowGeneratedID {
		id = ids.New("mcp")
	}
	if id == "" && name != "" {
		id = name
	}
	if name == "" {
		name = id
	}
	if id == "" {
		return core.MCPServer{}, fmt.Errorf("mcp server id or name is required")
	}
	transport := strings.ToLower(strings.TrimSpace(os.ExpandEnv(input.Transport)))
	if transport == "" {
		return core.MCPServer{}, fmt.Errorf("mcp server %s transport is required", id)
	}
	command := strings.TrimSpace(os.ExpandEnv(input.Command))
	targetURL := strings.TrimSpace(os.ExpandEnv(input.URL))
	switch transport {
	case TransportStdio:
		if command == "" {
			return core.MCPServer{}, fmt.Errorf("mcp stdio server %s command is required", id)
		}
	case TransportHTTP, TransportSSE:
		if targetURL == "" {
			return core.MCPServer{}, fmt.Errorf("mcp http server %s url is required", id)
		}
		parsed, err := url.ParseRequestURI(targetURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return core.MCPServer{}, fmt.Errorf("mcp http server %s url is invalid", id)
		}
		if input.MessageURL != "" {
			parsed, err = url.ParseRequestURI(strings.TrimSpace(os.ExpandEnv(input.MessageURL)))
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				return core.MCPServer{}, fmt.Errorf("mcp http server %s message_url is invalid", id)
			}
		}
	default:
		return core.MCPServer{}, fmt.Errorf("unsupported mcp transport for server %s: %s", id, transport)
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	timeout := input.TimeoutSeconds
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	compatibility, err := normalizeCompatibility(input, timeout)
	if err != nil {
		return core.MCPServer{}, fmt.Errorf("mcp server %s compatibility: %w", id, err)
	}
	now := time.Now().UTC()
	return core.MCPServer{
		ID:             id,
		Name:           name,
		Transport:      transport,
		Dialect:        compatibility.Dialect,
		Compatibility:  compatibility,
		Command:        command,
		Args:           expandStrings(input.Args),
		Cwd:            strings.TrimSpace(os.ExpandEnv(input.Cwd)),
		URL:            targetURL,
		MessageURL:     strings.TrimSpace(os.ExpandEnv(input.MessageURL)),
		Headers:        expandStringMap(input.Headers),
		Enabled:        enabled,
		Environment:    expandStringMap(input.Env),
		TimeoutSeconds: timeout,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func normalizeCompatibility(input ServerInput, timeout int) (core.MCPCompatibilityProfile, error) {
	profile := defaultCompatibilityProfile(timeout)
	dialect := strings.ToLower(strings.TrimSpace(os.ExpandEnv(input.Dialect)))
	if dialect == "" {
		dialect = strings.ToLower(strings.TrimSpace(os.ExpandEnv(input.Compatibility.Dialect)))
	}
	if dialect != "" {
		profile.Dialect = dialect
	}
	switch profile.Dialect {
	case DialectStrictJSONRPC, DialectLineDelimitedJSONRPC, DialectEnvelopeWrapped:
	default:
		return core.MCPCompatibilityProfile{}, fmt.Errorf("unsupported dialect: %s", profile.Dialect)
	}

	if input.Compatibility.AcceptMissingSchema != nil {
		profile.AcceptMissingSchema = *input.Compatibility.AcceptMissingSchema
	}
	if input.Compatibility.AcceptExtraMetadata != nil {
		profile.AcceptExtraMetadata = *input.Compatibility.AcceptExtraMetadata
	}
	if input.Compatibility.AcceptTextOnlyResult != nil {
		profile.AcceptTextOnlyResult = *input.Compatibility.AcceptTextOnlyResult
	}
	if input.Compatibility.AcceptStructuredResult != nil {
		profile.AcceptStructuredResult = *input.Compatibility.AcceptStructuredResult
	}
	if input.Compatibility.NormalizeErrorShape != nil {
		profile.NormalizeErrorShape = *input.Compatibility.NormalizeErrorShape
	}
	if input.Compatibility.StrictIDMatching != nil {
		profile.StrictIDMatching = *input.Compatibility.StrictIDMatching
	}
	if input.Compatibility.MaxPayloadBytes > 0 {
		profile.MaxPayloadBytes = input.Compatibility.MaxPayloadBytes
	}
	if input.Compatibility.TimeoutSeconds > 0 {
		profile.TimeoutSeconds = input.Compatibility.TimeoutSeconds
	}
	if profile.MaxPayloadBytes <= 0 {
		return core.MCPCompatibilityProfile{}, fmt.Errorf("max_payload_bytes must be positive")
	}
	if profile.TimeoutSeconds <= 0 {
		return core.MCPCompatibilityProfile{}, fmt.Errorf("timeout_seconds must be positive")
	}
	return profile, nil
}

func defaultCompatibilityProfile(timeout int) core.MCPCompatibilityProfile {
	if timeout <= 0 {
		timeout = 30
	}
	return core.MCPCompatibilityProfile{
		Dialect:                DialectStrictJSONRPC,
		AcceptStructuredResult: true,
		NormalizeErrorShape:    true,
		StrictIDMatching:       true,
		MaxPayloadBytes:        DefaultMaxPayloadBytes,
		TimeoutSeconds:         timeout,
	}
}

func compatibilityInputEmpty(input CompatibilityInput) bool {
	return input.Dialect == "" &&
		input.AcceptMissingSchema == nil &&
		input.AcceptExtraMetadata == nil &&
		input.AcceptTextOnlyResult == nil &&
		input.AcceptStructuredResult == nil &&
		input.NormalizeErrorShape == nil &&
		input.StrictIDMatching == nil &&
		input.MaxPayloadBytes == 0 &&
		input.TimeoutSeconds == 0
}

func compatibilityToInput(profile core.MCPCompatibilityProfile) CompatibilityInput {
	return CompatibilityInput{
		Dialect:                profile.Dialect,
		AcceptMissingSchema:    boolPtr(profile.AcceptMissingSchema),
		AcceptExtraMetadata:    boolPtr(profile.AcceptExtraMetadata),
		AcceptTextOnlyResult:   boolPtr(profile.AcceptTextOnlyResult),
		AcceptStructuredResult: boolPtr(profile.AcceptStructuredResult),
		NormalizeErrorShape:    boolPtr(profile.NormalizeErrorShape),
		StrictIDMatching:       boolPtr(profile.StrictIDMatching),
		MaxPayloadBytes:        profile.MaxPayloadBytes,
		TimeoutSeconds:         profile.TimeoutSeconds,
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func normalizePolicy(id string, input ToolPolicyInput) (core.MCPToolPolicy, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = strings.TrimSpace(input.ID)
	}
	toolName := strings.TrimSpace(input.ToolName)
	if toolName == "" {
		toolName = id
	}
	if id == "" {
		return core.MCPToolPolicy{}, fmt.Errorf("mcp tool policy id is required")
	}
	approval := strings.ToLower(strings.TrimSpace(input.Approval))
	requiresApproval := false
	if input.RequiresApproval != nil {
		requiresApproval = *input.RequiresApproval
	}
	switch approval {
	case "", ApprovalAuto:
		if requiresApproval {
			approval = ApprovalRequire
		} else {
			approval = ApprovalAuto
		}
	case ApprovalRequire:
		requiresApproval = true
	default:
		return core.MCPToolPolicy{}, fmt.Errorf("invalid mcp policy approval for %s: %s", id, approval)
	}

	effects := normalizeEffects(input.Effects)
	risk := strings.TrimSpace(input.RiskLevel)
	if risk == "" {
		risk = riskFromEffects(effects)
	}
	return core.MCPToolPolicy{
		ID:               id,
		ToolName:         toolName,
		Effects:          effects,
		Approval:         approval,
		RequiresApproval: requiresApproval,
		RiskLevel:        risk,
		Reason:           input.Reason,
		UpdatedAt:        time.Now().UTC(),
	}, nil
}

func profileFromPolicy(serverID, toolName string, policy core.MCPToolPolicy) core.MCPPolicyProfile {
	effects := normalizeEffects(policy.Effects)
	if len(effects) == 0 {
		effects = []string{"unknown.effect"}
	}
	approval := policy.Approval
	if approval == "" {
		approval = ApprovalAuto
		if policy.RequiresApproval {
			approval = ApprovalRequire
		}
	}
	requiresApproval := policy.RequiresApproval || approval == ApprovalRequire
	return core.MCPPolicyProfile{
		ServerID:         serverID,
		ToolName:         toolName,
		Effects:          effects,
		Approval:         approval,
		RequiresApproval: requiresApproval,
		RiskLevel:        nonEmpty(policy.RiskLevel, riskFromEffects(effects)),
		Reason:           nonEmpty(policy.Reason, "MCP local policy override"),
		Known:            true,
		Enabled:          true,
		Confidence:       0.98,
	}
}

func profileFromSchema(serverID, toolName string, schema MCPToolSchema) core.MCPPolicyProfile {
	effects := extractEffects(schema.Metadata)
	approval := extractString(schema.Metadata, "approval")
	requiresApproval := approval == ApprovalRequire || extractBool(schema.Metadata, "requires_approval")
	confidence := extractFloat(schema.Metadata, "confidence", 0.8)
	reason := "MCP server schema metadata"

	if len(effects) == 0 {
		if annotations, ok := schema.Metadata["annotations"].(map[string]any); ok && extractBool(annotations, "readOnlyHint") {
			effects = []string{"mcp.read"}
			approval = ApprovalAuto
			confidence = 0.8
			reason = "MCP server read-only annotation"
		} else {
			effects = []string{"unknown.effect"}
			approval = ApprovalRequire
			requiresApproval = true
			confidence = 0.45
			reason = "MCP server schema lacks local effect override"
		}
	}
	if approval == "" {
		approval = ApprovalAuto
	}
	if confidence > 0.8 {
		confidence = 0.8
	}
	if containsEffect(effects, "unknown.effect") {
		requiresApproval = true
		approval = ApprovalRequire
	}
	return core.MCPPolicyProfile{
		ServerID:         serverID,
		ToolName:         toolName,
		Effects:          effects,
		Approval:         approval,
		RequiresApproval: requiresApproval,
		RiskLevel:        riskFromEffects(effects),
		Reason:           reason,
		Known:            true,
		Enabled:          true,
		Confidence:       confidence,
	}
}

func serverToInput(server core.MCPServer) ServerInput {
	enabled := server.Enabled
	return ServerInput{
		ID:             server.ID,
		Name:           server.Name,
		Transport:      server.Transport,
		Dialect:        server.Dialect,
		Compatibility:  compatibilityToInput(server.Compatibility),
		Command:        server.Command,
		Args:           append([]string(nil), server.Args...),
		Cwd:            server.Cwd,
		URL:            server.URL,
		MessageURL:     server.MessageURL,
		Headers:        copyStringMap(server.Headers),
		Enabled:        &enabled,
		Env:            copyStringMap(server.Environment),
		TimeoutSeconds: server.TimeoutSeconds,
	}
}

func sanitizeServer(server core.MCPServer) core.MCPServer {
	server.Headers = redactStringMap(server.Headers)
	server.Environment = redactStringMap(server.Environment)
	server.Command = security.RedactString(server.Command)
	server.Args = expandStrings(server.Args)
	for i := range server.Args {
		server.Args[i] = security.RedactString(server.Args[i])
	}
	return server
}

func expandStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, os.ExpandEnv(item))
	}
	return out
}

func expandStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range input {
		out[key] = os.ExpandEnv(value)
	}
	return out
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func redactStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range input {
		if isSensitiveKey(key) {
			out[key] = "[REDACTED]"
		} else {
			out[key] = security.RedactString(value)
		}
	}
	return out
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	return core.CloneMap(input)
}

func sortedSchemas(input map[string]MCPToolSchema) []MCPToolSchema {
	items := make([]MCPToolSchema, 0, len(input))
	for _, schema := range input {
		schema.InputSchema = cloneMap(schema.InputSchema)
		schema.Metadata = cloneMap(schema.Metadata)
		items = append(items, schema)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func normalizeEffects(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		effect := strings.TrimSpace(item)
		if effect == "" || seen[effect] {
			continue
		}
		seen[effect] = true
		out = append(out, effect)
	}
	return out
}

func extractEffects(metadata map[string]any) []string {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata["effects"]
	if !ok {
		raw = metadata["effect"]
	}
	switch value := raw.(type) {
	case string:
		return normalizeEffects([]string{value})
	case []string:
		return normalizeEffects(value)
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return normalizeEffects(items)
	default:
		return nil
	}
}

func extractString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.ToLower(strings.TrimSpace(value))
}

func extractBool(metadata map[string]any, key string) bool {
	if metadata == nil {
		return false
	}
	value, _ := metadata[key].(bool)
	return value
}

func extractFloat(metadata map[string]any, key string, fallback float64) float64 {
	if metadata == nil {
		return fallback
	}
	switch value := metadata[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return fallback
	}
}

func riskFromEffects(effects []string) string {
	if len(effects) == 0 || containsEffect(effects, "unknown.effect") {
		return "unknown"
	}
	risk := "read"
	for _, effect := range effects {
		switch {
		case strings.Contains(effect, "sensitive") || strings.Contains(effect, "env_file"):
			return "sensitive"
		case strings.Contains(effect, "kill") || strings.Contains(effect, "restart") || strings.Contains(effect, "stop") || strings.Contains(effect, "escalate") || strings.Contains(effect, "danger"):
			return "danger"
		case strings.Contains(effect, "write") || strings.Contains(effect, "modify") || strings.Contains(effect, "delete") || strings.Contains(effect, "install") || effect == "network.post" || effect == "network.put" || effect == "network.delete":
			risk = "write"
		}
	}
	return risk
}

func containsEffect(effects []string, needle string) bool {
	for _, effect := range effects {
		if effect == needle {
			return true
		}
	}
	return false
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "password") ||
		strings.Contains(lower, "authorization") ||
		strings.Contains(lower, "cookie") ||
		strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "apikey")
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

// DefaultTransportFactory creates built-in MCP transports.
type DefaultTransportFactory struct{}

// NewTransport creates stdio or HTTP transports.
func (DefaultTransportFactory) NewTransport(cfg TransportConfig) (MCPTransport, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Transport)) {
	case TransportStdio:
		return NewStdioTransport(cfg), nil
	case TransportHTTP:
		return NewHTTPTransport(cfg), nil
	case TransportSSE:
		return NewSSETransport(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported mcp transport: %s", cfg.Transport)
	}
}
