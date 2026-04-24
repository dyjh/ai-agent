package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ValidationResult is the preflight report returned by skill validation endpoints.
type ValidationResult struct {
	SkillID                 string           `json:"skill_id"`
	Version                 string           `json:"version"`
	Valid                   bool             `json:"valid"`
	Status                  string           `json:"status"`
	Runner                  string           `json:"runner,omitempty"`
	RequestedSandboxProfile string           `json:"requested_sandbox_profile,omitempty"`
	SandboxProfile          string           `json:"sandbox_profile,omitempty"`
	PlatformSupported       bool             `json:"platform_supported"`
	WillFallback            bool             `json:"will_fallback"`
	RequiresApproval        bool             `json:"requires_approval"`
	StrongIsolation         bool             `json:"strong_isolation"`
	Warnings                []string         `json:"warnings,omitempty"`
	ExecutionProfile        ExecutionProfile `json:"execution_profile"`
}

type preparedExecution struct {
	Request SandboxRequest
	Profile ExecutionProfile
	Runner  SandboxRunner
}

type sandboxSelection struct {
	Runner             SandboxRunner
	EffectiveProfile   SandboxProfile
	PlatformSupported  bool
	WillFallback       bool
	RequiresApproval   bool
	StrongIsolation    bool
	FilesystemEnforced bool
	NetworkEnforced    bool
	RootFS             string
	MaxProcesses       int
	MemoryLimitMB      int
	Warnings           []string
}

func prepareExecution(entry RegisteredSkill, args map[string]any, defaultMaxOutput int64, sandboxes []SandboxRunner) (preparedExecution, error) {
	profile, runner, err := buildExecutionProfile(entry, defaultMaxOutput, sandboxes)
	if err != nil {
		return preparedExecution{}, err
	}

	payload, err := json.Marshal(args)
	if err != nil {
		return preparedExecution{}, fmt.Errorf("marshal skill args: %w", err)
	}

	commandPath, err := resolveRuntimePath(entry.Root, entry.Manifest.Runtime.Command)
	if err != nil {
		return preparedExecution{}, fmt.Errorf("resolve runtime.command: %w", err)
	}
	cwdPath, err := resolveRuntimePath(entry.Root, entry.Manifest.Runtime.Cwd)
	if err != nil {
		return preparedExecution{}, fmt.Errorf("resolve runtime.cwd: %w", err)
	}
	if err := validateRuntimePath("runtime.command", commandPath, append(profile.AllowedReadPaths, profile.AllowedWritePaths...)); err != nil {
		return preparedExecution{}, err
	}
	if err := validateRuntimePath("runtime.cwd", cwdPath, append(profile.AllowedReadPaths, profile.AllowedWritePaths...)); err != nil {
		return preparedExecution{}, err
	}
	if err := validateCommandPath(commandPath); err != nil {
		return preparedExecution{}, err
	}
	if err := validateDirPath(cwdPath); err != nil {
		return preparedExecution{}, err
	}
	if err := validateInputPaths(entry.Root, args, profile); err != nil {
		return preparedExecution{}, err
	}

	commandArgs := append([]string(nil), entry.Manifest.Runtime.Args...)
	if entry.Manifest.Runtime.InputMode == InputModeArgs {
		commandArgs = append(commandArgs, argsToFlags(args)...)
	}

	env := buildSandboxEnv(profile.AllowedEnv, payload, args, entry.Manifest.Runtime.InputMode)
	requestCommand := commandPath
	requestCWD := cwdPath
	if profile.RootFS != "" {
		requestCommand, err = sandboxPath(commandPath, profile.RootFS)
		if err != nil {
			return preparedExecution{}, fmt.Errorf("normalize runtime.command for sandbox: %w", err)
		}
		requestCWD, err = sandboxPath(cwdPath, profile.RootFS)
		if err != nil {
			return preparedExecution{}, fmt.Errorf("normalize runtime.cwd for sandbox: %w", err)
		}
	}

	request := SandboxRequest{
		Command: requestCommand,
		Args:    commandArgs,
		CWD:     requestCWD,
		Env:     env,
		Profile: profile,
	}
	if entry.Manifest.Runtime.InputMode == InputModeJSONStdin {
		request.Stdin = payload
	}
	return preparedExecution{
		Request: request,
		Profile: profile,
		Runner:  runner,
	}, nil
}

func validateExecution(entry RegisteredSkill, args map[string]any, defaultMaxOutput int64, sandboxes []SandboxRunner) (ValidationResult, error) {
	if err := ValidateInput(entry.Manifest.InputSchema, args); err != nil {
		return ValidationResult{}, fmt.Errorf("skill input validation failed: %w", err)
	}
	prepared, err := prepareExecution(entry, args, defaultMaxOutput, sandboxes)
	if err != nil {
		return ValidationResult{}, err
	}
	status := "ok"
	if prepared.Profile.WillFallback || prepared.Profile.RequiresApproval || len(prepared.Profile.Warnings) > 0 {
		status = "warning"
	}
	return ValidationResult{
		SkillID:                 entry.Registration.ID,
		Version:                 entry.Registration.Version,
		Valid:                   true,
		Status:                  status,
		Runner:                  prepared.Profile.Runner,
		RequestedSandboxProfile: prepared.Profile.RequestedSandboxProfile,
		SandboxProfile:          prepared.Profile.SandboxProfile,
		PlatformSupported:       prepared.Profile.PlatformSupported,
		WillFallback:            prepared.Profile.WillFallback,
		RequiresApproval:        prepared.Profile.RequiresApproval,
		StrongIsolation:         prepared.Profile.StrongIsolation,
		Warnings:                append([]string(nil), prepared.Profile.Warnings...),
		ExecutionProfile:        prepared.Profile,
	}, nil
}

func buildExecutionProfile(entry RegisteredSkill, defaultMaxOutput int64, sandboxes []SandboxRunner) (ExecutionProfile, SandboxRunner, error) {
	if entry.Manifest.Permissions.Tools.AllowShell {
		return ExecutionProfile{}, nil, fmt.Errorf("permissions.tools.allow_shell is not supported by the current skill runners")
	}
	if entry.Manifest.Permissions.Tools.AllowMCP {
		return ExecutionProfile{}, nil, fmt.Errorf("permissions.tools.allow_mcp is not supported by the current skill runners")
	}

	root, err := filepath.Abs(entry.Root)
	if err != nil {
		return ExecutionProfile{}, nil, err
	}
	readPaths, err := resolvePermissionPaths(root, entry.Manifest.Permissions.Filesystem.Read)
	if err != nil {
		return ExecutionProfile{}, nil, fmt.Errorf("resolve permissions.filesystem.read: %w", err)
	}
	writePaths, err := resolvePermissionPaths(root, entry.Manifest.Permissions.Filesystem.Write)
	if err != nil {
		return ExecutionProfile{}, nil, fmt.Errorf("resolve permissions.filesystem.write: %w", err)
	}
	if !pathWithinAny(root, readPaths) {
		readPaths = append(readPaths, root)
	}

	timeout := entry.Manifest.Runtime.TimeoutSeconds
	if timeout <= 0 || entry.Manifest.Permissions.Process.MaxRuntimeSeconds < timeout {
		timeout = entry.Manifest.Permissions.Process.MaxRuntimeSeconds
	}
	if timeout <= 0 {
		timeout = 30
	}

	maxOutput := entry.Manifest.Permissions.Process.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = defaultProcessMaxOutputBytes
	}
	if defaultMaxOutput > 0 && int64(defaultMaxOutput) < maxOutput {
		maxOutput = int64(defaultMaxOutput)
	}

	allowedEnv := append(DefaultEnvAllowList(), entry.Manifest.Permissions.Env.Allow...)
	allowedEnv = normalizeStringList(allowedEnv, true)

	requestedProfile := entry.Manifest.Sandbox.Profile
	if requestedProfile == "" {
		requestedProfile = SandboxProfileRestricted
	}
	selection, err := selectSandbox(entry, root, readPaths, writePaths, sandboxes)
	if err != nil {
		return ExecutionProfile{}, nil, err
	}
	if selection.StrongIsolation && selection.RootFS != "" {
		commandPath, err := resolveRuntimePath(root, entry.Manifest.Runtime.Command)
		if err != nil {
			return ExecutionProfile{}, nil, fmt.Errorf("resolve runtime.command: %w", err)
		}
		compatible, reason, err := commandSupportsChroot(commandPath, selection.RootFS)
		if err != nil {
			return ExecutionProfile{}, nil, err
		}
		if !compatible {
			if requestedProfile == SandboxProfileLinuxIsolated {
				return ExecutionProfile{}, nil, fmt.Errorf("sandbox.profile=%s requires a rootfs-compatible runtime.command: %s", requestedProfile, reason)
			}
			selection.FilesystemEnforced = false
			selection.RootFS = ""
			selection.WillFallback = true
			selection.RequiresApproval = true
			selection.Warnings = append(selection.Warnings, reason, "filesystem isolation is unavailable for this runtime.command; approval is required")
		}
	}

	return ExecutionProfile{
		RequestedSandboxProfile: string(requestedProfile),
		SandboxProfile:          string(selection.EffectiveProfile),
		Runner:                  selection.Runner.Name(),
		Platform:                runtime.GOOS,
		PlatformSupported:       selection.PlatformSupported,
		WillFallback:            selection.WillFallback,
		RequiresApproval:        selection.RequiresApproval,
		StrongIsolation:         selection.StrongIsolation,
		FilesystemEnforced:      selection.FilesystemEnforced,
		NetworkEnforced:         selection.NetworkEnforced,
		AllowedReadPaths:        sortAndUniqPaths(readPaths),
		AllowedWritePaths:       sortAndUniqPaths(writePaths),
		NetworkEnabled:          entry.Manifest.Permissions.Network.Enabled,
		AllowedHosts:            append([]string(nil), entry.Manifest.Permissions.Network.AllowHosts...),
		AllowedEnv:              append([]string(nil), allowedEnv...),
		TimeoutSeconds:          timeout,
		MaxOutputBytes:          maxOutput,
		MaxProcesses:            selection.MaxProcesses,
		MemoryLimitMB:           selection.MemoryLimitMB,
		RootFS:                  selection.RootFS,
		Warnings:                selection.Warnings,
	}, selection.Runner, nil
}

func defaultSandboxRunners() []SandboxRunner {
	return []SandboxRunner{
		&LinuxIsolatedRunner{},
		&LocalRestrictedRunner{},
	}
}

func cloneSandboxRunners(items []SandboxRunner) []SandboxRunner {
	if len(items) == 0 {
		return defaultSandboxRunners()
	}
	out := make([]SandboxRunner, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return defaultSandboxRunners()
	}
	return out
}

func selectSandbox(entry RegisteredSkill, root string, readPaths, writePaths []string, sandboxes []SandboxRunner) (sandboxSelection, error) {
	sandboxes = cloneSandboxRunners(sandboxes)
	requested := entry.Manifest.Sandbox.Profile
	if requested == "" {
		requested = SandboxProfileRestricted
	}

	localRunner := firstSandboxRunner(sandboxes, SandboxProfileBestEffortLocal)
	trustedRunner := firstSandboxRunner(sandboxes, SandboxProfileTrustedLocal)
	if trustedRunner == nil {
		trustedRunner = localRunner
	}
	linuxRunner := firstSandboxRunner(sandboxes, SandboxProfileLinuxIsolated)
	if linuxRunner == nil {
		linuxRunner = firstSandboxRunner(sandboxes, SandboxProfileRestricted)
	}

	switch requested {
	case SandboxProfileBestEffortLocal:
		return buildLocalSelection(entry, localRunner, requested, SandboxProfileBestEffortLocal, false, false, nil)
	case SandboxProfileTrustedLocal:
		return buildLocalSelection(entry, trustedRunner, requested, SandboxProfileTrustedLocal, false, false, []string{
			"trusted_local executes on the host after preflight validation and does not provide OS-level isolation",
		})
	case SandboxProfileRestricted:
		if linuxRunner != nil {
			if err := validateLinuxSandboxScope(root, readPaths, writePaths); err == nil {
				return buildLinuxSelection(entry, root, linuxRunner, requested, SandboxProfileRestricted)
			} else if localRunner != nil {
				return buildLocalSelection(entry, localRunner, requested, SandboxProfileBestEffortLocal, true, true, []string{
					err.Error(),
					"falling back to best_effort_local; approval is required before execution",
				})
			} else {
				return sandboxSelection{}, err
			}
		}
		return buildLocalSelection(entry, localRunner, requested, SandboxProfileBestEffortLocal, false, true, []string{
			"linux isolated sandbox is unavailable; falling back to best_effort_local and requiring approval",
		})
	case SandboxProfileLinuxIsolated:
		if linuxRunner == nil {
			return sandboxSelection{}, fmt.Errorf("sandbox.profile=%s is not supported by the configured skill runners", requested)
		}
		if err := validateLinuxSandboxScope(root, readPaths, writePaths); err != nil {
			return sandboxSelection{}, err
		}
		if len(entry.Manifest.Permissions.Network.AllowHosts) > 0 {
			return sandboxSelection{}, fmt.Errorf("sandbox.profile=%s does not support permissions.network.allow_hosts enforcement", requested)
		}
		return buildLinuxSelection(entry, root, linuxRunner, requested, SandboxProfileLinuxIsolated)
	default:
		return sandboxSelection{}, fmt.Errorf("unsupported skill sandbox.profile: %s", requested)
	}
}

func buildLocalSelection(entry RegisteredSkill, runner SandboxRunner, requested, effective SandboxProfile, platformSupported, requireApproval bool, warnings []string) (sandboxSelection, error) {
	if runner == nil {
		return sandboxSelection{}, fmt.Errorf("no local sandbox runner is configured for %s", effective)
	}

	mergedWarnings := append([]string(nil), warnings...)
	if requested == SandboxProfileRestricted || requested == SandboxProfileLinuxIsolated {
		mergedWarnings = append(mergedWarnings, "local skill sandbox is best-effort; filesystem and network restrictions rely on preflight validation, not OS-level isolation")
	}
	if !entry.Manifest.Permissions.Network.Enabled {
		mergedWarnings = append(mergedWarnings, "network disable is best-effort only in the local runner")
	} else if len(entry.Manifest.Permissions.Network.AllowHosts) > 0 {
		mergedWarnings = append(mergedWarnings, "network allow_hosts is declarative in the local runner")
	}

	return sandboxSelection{
		Runner:             runner,
		EffectiveProfile:   effective,
		PlatformSupported:  platformSupported || requested == SandboxProfileBestEffortLocal || requested == SandboxProfileTrustedLocal,
		WillFallback:       requested != effective,
		RequiresApproval:   requireApproval,
		StrongIsolation:    false,
		FilesystemEnforced: false,
		NetworkEnforced:    false,
		Warnings:           mergedWarnings,
	}, nil
}

func buildLinuxSelection(entry RegisteredSkill, root string, runner SandboxRunner, requested, effective SandboxProfile) (sandboxSelection, error) {
	if runner == nil {
		return sandboxSelection{}, fmt.Errorf("linux isolated sandbox runner is not configured")
	}

	if err := linuxIsolationSupportError(); err != nil {
		return sandboxSelection{}, err
	}

	warnings := []string{}
	requiresApproval := false
	networkEnforced := false
	if !entry.Manifest.Permissions.Network.Enabled {
		networkEnforced = true
	} else if len(entry.Manifest.Permissions.Network.AllowHosts) > 0 {
		requiresApproval = true
		warnings = append(warnings, "network allow_hosts is not enforced by the current linux isolated runner; approval is required")
	}

	return sandboxSelection{
		Runner:             runner,
		EffectiveProfile:   effective,
		PlatformSupported:  true,
		WillFallback:       false,
		RequiresApproval:   requiresApproval,
		StrongIsolation:    true,
		FilesystemEnforced: true,
		NetworkEnforced:    networkEnforced,
		RootFS:             root,
		Warnings:           warnings,
	}, nil
}

func firstSandboxRunner(items []SandboxRunner, profile SandboxProfile) SandboxRunner {
	for _, item := range items {
		if item != nil && item.Supports(profile) {
			return item
		}
	}
	return nil
}

func validateLinuxSandboxScope(root string, readPaths, writePaths []string) error {
	if err := linuxIsolationSupportError(); err != nil {
		return err
	}
	for _, path := range append(append([]string(nil), readPaths...), writePaths...) {
		if !pathWithinBase(path, root) {
			return fmt.Errorf("linux isolated sandbox only supports filesystem paths under the skill root")
		}
	}
	return nil
}

func sandboxPath(path, root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("sandbox root is required")
	}
	if !pathWithinBase(path, root) {
		return "", fmt.Errorf("%q is outside the sandbox root %q", path, root)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "/", nil
	}
	return "/" + filepath.ToSlash(rel), nil
}

func commandSupportsChroot(commandPath, root string) (bool, string, error) {
	data, err := os.ReadFile(commandPath)
	if err != nil {
		return false, "", fmt.Errorf("read runtime.command %q: %w", commandPath, err)
	}
	line := string(data)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	if !strings.HasPrefix(line, "#!") {
		return true, "", nil
	}
	parts := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "#!")))
	if len(parts) == 0 {
		return false, "runtime.command has an invalid shebang", nil
	}
	interpreter := parts[0]
	if pathWithinBase(interpreter, root) {
		return true, "", nil
	}
	return false, fmt.Sprintf("runtime.command %q depends on interpreter %q outside the skill root", commandPath, interpreter), nil
}

func resolvePermissionPaths(root string, items []string) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		resolved, err := resolveRuntimePath(root, item)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved)
	}
	return out, nil
}

func resolveRuntimePath(root, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.HasPrefix(value, "~") {
		return "", fmt.Errorf("home-relative paths are not supported: %s", value)
	}
	var candidate string
	if filepath.IsAbs(value) {
		candidate = value
	} else {
		candidate = filepath.Join(root, value)
	}
	return filepath.Abs(filepath.Clean(candidate))
}

func validateRuntimePath(label, path string, allowed []string) error {
	if len(allowed) == 0 {
		return fmt.Errorf("%s %q is outside the allowed filesystem scope", label, path)
	}
	if !pathWithinAny(path, allowed) {
		return fmt.Errorf("%s %q is outside the allowed filesystem scope", label, path)
	}
	return nil
}

func validateCommandPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("runtime.command %q is unavailable: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("runtime.command %q must be a file", path)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("runtime.command %q is not executable", path)
	}
	return nil
}

func validateDirPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("runtime.cwd %q is unavailable: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("runtime.cwd %q must be a directory", path)
	}
	return nil
}

func buildSandboxEnv(allowed []string, payload []byte, args map[string]any, inputMode string) map[string]string {
	out := map[string]string{}
	for _, key := range allowed {
		if value, ok := os.LookupEnv(key); ok {
			out[key] = value
		}
	}
	out["SKILL_INPUT_JSON"] = string(payload)
	if inputMode == InputModeEnv {
		for _, item := range buildEnvArgs(payload, args) {
			parts := strings.SplitN(item, "=", 2)
			if len(parts) == 2 {
				out[parts[0]] = parts[1]
			}
		}
	}
	return out
}

func validateInputPaths(root string, value any, profile ExecutionProfile) error {
	return walkInputPaths("", value, func(keyPath, item string) error {
		if !looksLikeLocalPath(item) {
			return nil
		}
		resolved, err := resolveRuntimePath(root, item)
		if err != nil {
			return fmt.Errorf("%s: %w", keyPath, err)
		}
		switch classifyPathAccess(keyPath) {
		case "write":
			if len(profile.AllowedWritePaths) == 0 || !pathWithinAny(resolved, profile.AllowedWritePaths) {
				return fmt.Errorf("%s %q is outside allowed write paths", keyPath, item)
			}
		case "read":
			allowed := append([]string(nil), profile.AllowedReadPaths...)
			allowed = append(allowed, profile.AllowedWritePaths...)
			if !pathWithinAny(resolved, allowed) {
				return fmt.Errorf("%s %q is outside allowed read paths", keyPath, item)
			}
		default:
			return nil
		}
		return nil
	})
}

func walkInputPaths(prefix string, value any, visit func(keyPath, item string) error) error {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			if err := walkInputPaths(next, typed[key], visit); err != nil {
				return err
			}
		}
	case []any:
		for idx, item := range typed {
			next := fmt.Sprintf("%s[%d]", prefix, idx)
			if err := walkInputPaths(next, item, visit); err != nil {
				return err
			}
		}
	case string:
		if prefix == "" {
			prefix = "$"
		}
		return visit(prefix, typed)
	}
	return nil
}

func classifyPathAccess(keyPath string) string {
	keyPath = strings.ToLower(keyPath)
	switch {
	case strings.Contains(keyPath, "output"),
		strings.Contains(keyPath, "dest"),
		strings.Contains(keyPath, "destination"),
		strings.Contains(keyPath, "target"),
		strings.Contains(keyPath, "write"),
		strings.Contains(keyPath, "save"):
		return "write"
	case strings.Contains(keyPath, "path"),
		strings.Contains(keyPath, "file"),
		strings.Contains(keyPath, "dir"),
		strings.Contains(keyPath, "cwd"),
		strings.Contains(keyPath, "input"),
		strings.Contains(keyPath, "source"),
		strings.Contains(keyPath, "read"):
		return "read"
	default:
		return ""
	}
}

func looksLikeLocalPath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "://") {
		return false
	}
	return strings.HasPrefix(value, ".") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") || strings.Contains(value, string(filepath.Separator))
}

func pathWithinAny(path string, allowed []string) bool {
	for _, base := range allowed {
		if pathWithinBase(path, base) {
			return true
		}
	}
	return false
}

func pathWithinBase(path, base string) bool {
	path = filepath.Clean(path)
	base = filepath.Clean(base)
	if path == base {
		return true
	}
	return strings.HasPrefix(path, base+string(filepath.Separator))
}

func sortAndUniqPaths(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, item := range paths {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
