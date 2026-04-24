package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ValidationResult is the preflight report returned by skill validation endpoints.
type ValidationResult struct {
	SkillID          string           `json:"skill_id"`
	Version          string           `json:"version"`
	Status           string           `json:"status"`
	Warnings         []string         `json:"warnings,omitempty"`
	ExecutionProfile ExecutionProfile `json:"execution_profile"`
}

type preparedExecution struct {
	Request SandboxRequest
	Profile ExecutionProfile
}

func prepareExecution(entry RegisteredSkill, args map[string]any, defaultMaxOutput int64) (preparedExecution, error) {
	profile, err := buildExecutionProfile(entry, defaultMaxOutput)
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
	request := SandboxRequest{
		Command: commandPath,
		Args:    commandArgs,
		CWD:     cwdPath,
		Env:     env,
		Profile: profile,
	}
	if entry.Manifest.Runtime.InputMode == InputModeJSONStdin {
		request.Stdin = payload
	}
	return preparedExecution{
		Request: request,
		Profile: profile,
	}, nil
}

func validateExecution(entry RegisteredSkill, args map[string]any, defaultMaxOutput int64) (ValidationResult, error) {
	if err := ValidateInput(entry.Manifest.InputSchema, args); err != nil {
		return ValidationResult{}, fmt.Errorf("skill input validation failed: %w", err)
	}
	prepared, err := prepareExecution(entry, args, defaultMaxOutput)
	if err != nil {
		return ValidationResult{}, err
	}
	return ValidationResult{
		SkillID:          entry.Registration.ID,
		Version:          entry.Registration.Version,
		Status:           "ok",
		Warnings:         append([]string(nil), prepared.Profile.Warnings...),
		ExecutionProfile: prepared.Profile,
	}, nil
}

func buildExecutionProfile(entry RegisteredSkill, defaultMaxOutput int64) (ExecutionProfile, error) {
	if entry.Manifest.Permissions.Tools.AllowShell {
		return ExecutionProfile{}, fmt.Errorf("permissions.tools.allow_shell is not supported by the current local runner")
	}
	if entry.Manifest.Permissions.Tools.AllowMCP {
		return ExecutionProfile{}, fmt.Errorf("permissions.tools.allow_mcp is not supported by the current local runner")
	}

	root, err := filepath.Abs(entry.Root)
	if err != nil {
		return ExecutionProfile{}, err
	}
	readPaths, err := resolvePermissionPaths(root, entry.Manifest.Permissions.Filesystem.Read)
	if err != nil {
		return ExecutionProfile{}, fmt.Errorf("resolve permissions.filesystem.read: %w", err)
	}
	writePaths, err := resolvePermissionPaths(root, entry.Manifest.Permissions.Filesystem.Write)
	if err != nil {
		return ExecutionProfile{}, fmt.Errorf("resolve permissions.filesystem.write: %w", err)
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
	effectiveProfile := requestedProfile
	warnings := []string{}
	if requestedProfile != SandboxProfileBestEffort {
		effectiveProfile = SandboxProfileBestEffort
		warnings = append(warnings, "local skill sandbox is best-effort; filesystem and network restrictions are preflight checks, not OS-level isolation")
	}
	if !entry.Manifest.Permissions.Network.Enabled {
		warnings = append(warnings, "network disable is best-effort only in the local runner")
	} else if len(entry.Manifest.Permissions.Network.AllowHosts) > 0 {
		warnings = append(warnings, "network allow_hosts is declarative in the local runner")
	}

	return ExecutionProfile{
		RequestedSandboxProfile: requestedProfile,
		SandboxProfile:          effectiveProfile,
		AllowedReadPaths:        sortAndUniqPaths(readPaths),
		AllowedWritePaths:       sortAndUniqPaths(writePaths),
		NetworkEnabled:          entry.Manifest.Permissions.Network.Enabled,
		AllowedHosts:            append([]string(nil), entry.Manifest.Permissions.Network.AllowHosts...),
		AllowedEnv:              append([]string(nil), allowedEnv...),
		TimeoutSeconds:          timeout,
		MaxOutputBytes:          maxOutput,
		Warnings:                warnings,
	}, nil
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
