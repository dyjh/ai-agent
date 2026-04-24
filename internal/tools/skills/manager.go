package skills

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"local-agent/internal/core"
)

const (
	skillPackageMetaFile = ".skill-package.json"
	maxZipFiles          = 128
	maxZipTotalBytes     = int64(32 << 20)
	maxZipFileBytes      = int64(8 << 20)
)

// RegisteredSkill is the normalized registry record stored by the manager.
type RegisteredSkill struct {
	Registration core.SkillRegistration `json:"registration"`
	Manifest     Manifest               `json:"manifest"`
	Root         string                 `json:"root"`
	Package      core.SkillPackageInfo  `json:"package"`
}

// RemovalResult describes the outcome of uninstalling a skill registration.
type RemovalResult struct {
	SkillID        string                `json:"skill_id"`
	Version        string                `json:"version"`
	Removed        bool                  `json:"removed"`
	PackageDeleted bool                  `json:"package_deleted"`
	Package        core.SkillPackageInfo `json:"package"`
}

// Manager tracks locally registered skills.
type Manager struct {
	root         string
	packagesRoot string
	tempRoot     string
	sandboxes    []SandboxRunner
	mu           sync.RWMutex
	skills       map[string]RegisteredSkill
}

// NewManager creates a skill manager.
func NewManager(root string) *Manager {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	manager := &Manager{
		root:         absRoot,
		packagesRoot: filepath.Join(absRoot, "packages"),
		tempRoot:     filepath.Join(absRoot, ".tmp"),
		sandboxes:    defaultSandboxRunners(),
		skills:       map[string]RegisteredSkill{},
	}
	_ = os.MkdirAll(manager.root, 0o755)
	_ = os.MkdirAll(manager.packagesRoot, 0o755)
	_ = os.MkdirAll(manager.tempRoot, 0o755)
	manager.loadManagedPackages()
	return manager
}

// Upload registers a local skill directory or manifest path.
func (m *Manager) Upload(path, name, description string) (core.SkillRegistration, error) {
	if path == "" {
		return core.SkillRegistration{}, fmt.Errorf("skill path is required")
	}
	root, err := m.resolveRoot(path)
	if err != nil {
		return core.SkillRegistration{}, err
	}
	entry, err := m.loadEntry(root, localPackageInfo(root), name, description)
	if err != nil {
		return core.SkillRegistration{}, err
	}
	m.store(entry)
	return entry.Registration, nil
}

// InstallZip installs a skill package from a local zip archive.
func (m *Manager) InstallZip(path string, force bool) (RegisteredSkill, error) {
	if strings.TrimSpace(path) == "" {
		return RegisteredSkill{}, fmt.Errorf("zip path is required")
	}
	checksum, err := fileSHA256(path)
	if err != nil {
		return RegisteredSkill{}, err
	}

	workdir, err := os.MkdirTemp(m.tempRoot, "skillzip-*")
	if err != nil {
		return RegisteredSkill{}, err
	}
	defer os.RemoveAll(workdir)

	skillRoot, manifest, err := extractZipSkill(path, workdir)
	if err != nil {
		return RegisteredSkill{}, err
	}

	targetRoot := filepath.Join(m.packagesRoot, manifest.ID, manifest.Version)
	if _, err := os.Stat(targetRoot); err == nil {
		if !force {
			return RegisteredSkill{}, fmt.Errorf("skill %s version %s is already installed", manifest.ID, manifest.Version)
		}
		if err := os.RemoveAll(targetRoot); err != nil {
			return RegisteredSkill{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(targetRoot), 0o755); err != nil {
		return RegisteredSkill{}, err
	}
	if err := os.Rename(skillRoot, targetRoot); err != nil {
		return RegisteredSkill{}, err
	}

	info := core.SkillPackageInfo{
		SkillID:     manifest.ID,
		Version:     manifest.Version,
		SourceType:  "zip",
		PackagePath: targetRoot,
		Checksum:    checksum,
		InstalledAt: time.Now().UTC(),
	}
	if err := writePackageInfo(targetRoot, info); err != nil {
		_ = os.RemoveAll(targetRoot)
		return RegisteredSkill{}, err
	}

	entry, err := m.loadEntry(targetRoot, info, "", "")
	if err != nil {
		_ = os.RemoveAll(targetRoot)
		return RegisteredSkill{}, err
	}
	m.store(entry)
	return entry, nil
}

// List returns all registered skills.
func (m *Manager) List() []core.SkillRegistration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]core.SkillRegistration, 0, len(m.skills))
	for _, skill := range m.skills {
		items = append(items, skill.Registration)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

// SetEnabled updates the enabled state.
func (m *Manager) SetEnabled(id string, enabled bool) (core.SkillRegistration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.skills[id]
	if !ok {
		return core.SkillRegistration{}, fmt.Errorf("skill not found: %s", id)
	}
	item.Registration.Enabled = enabled
	m.skills[id] = item
	return item.Registration, nil
}

// Get returns a registered skill by ID.
func (m *Manager) Get(id string) (core.SkillRegistration, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.skills[id]
	if !ok {
		return core.SkillRegistration{}, fmt.Errorf("skill not found: %s", id)
	}
	return item.Registration, nil
}

// Package returns the install metadata for a registered skill.
func (m *Manager) Package(id string) (core.SkillPackageInfo, error) {
	item, err := m.Resolve(id)
	if err != nil {
		return core.SkillPackageInfo{}, err
	}
	return item.Package, nil
}

// Validate checks the registered skill manifest, runtime paths, and optional args.
func (m *Manager) Validate(id string, args map[string]any, defaultMaxOutput int64) (ValidationResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	item, err := m.Resolve(id)
	if err != nil {
		return ValidationResult{}, err
	}
	return validateExecution(item, args, defaultMaxOutput, m.AvailableSandboxes())
}

// Remove unregisters a skill and deletes managed zip packages when applicable.
func (m *Manager) Remove(id string) (RemovalResult, error) {
	m.mu.Lock()
	item, ok := m.skills[id]
	if !ok {
		m.mu.Unlock()
		return RemovalResult{}, fmt.Errorf("skill not found: %s", id)
	}
	delete(m.skills, id)
	m.mu.Unlock()

	result := RemovalResult{
		SkillID: item.Registration.ID,
		Version: item.Registration.Version,
		Removed: true,
		Package: item.Package,
	}
	if item.Package.SourceType == "zip" && item.Package.PackagePath != "" && pathWithinBase(item.Package.PackagePath, m.packagesRoot) {
		if err := os.RemoveAll(item.Package.PackagePath); err != nil {
			return result, err
		}
		result.PackageDeleted = true
	}
	return result, nil
}

// Resolve returns the full skill entry used by the runner.
func (m *Manager) Resolve(id string) (RegisteredSkill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.skills[id]
	if !ok {
		return RegisteredSkill{}, fmt.Errorf("skill not found: %s", id)
	}
	item.Registration.Effects = append([]string(nil), item.Registration.Effects...)
	item.Manifest.Effects = append([]string(nil), item.Manifest.Effects...)
	item.Manifest.Permissions.Filesystem.Read = append([]string(nil), item.Manifest.Permissions.Filesystem.Read...)
	item.Manifest.Permissions.Filesystem.Write = append([]string(nil), item.Manifest.Permissions.Filesystem.Write...)
	item.Manifest.Permissions.Network.AllowHosts = append([]string(nil), item.Manifest.Permissions.Network.AllowHosts...)
	item.Manifest.Permissions.Env.Allow = append([]string(nil), item.Manifest.Permissions.Env.Allow...)
	return item, nil
}

// PolicyProfile returns the policy metadata used by effect inference.
func (m *Manager) PolicyProfile(id string) (core.SkillPolicyProfile, error) {
	item, err := m.Resolve(id)
	if err != nil {
		return core.SkillPolicyProfile{}, err
	}
	profile, _, err := buildExecutionProfile(item, 0, m.AvailableSandboxes())
	if err != nil {
		return core.SkillPolicyProfile{}, err
	}
	return core.SkillPolicyProfile{
		ID:               item.Registration.ID,
		Effects:          append([]string(nil), item.Manifest.EffectiveEffects()...),
		ApprovalDefault:  item.Manifest.Approval.Default,
		Enabled:          item.Registration.Enabled,
		SandboxProfile:   profile.SandboxProfile,
		Runner:           profile.Runner,
		WillFallback:     profile.WillFallback,
		RequiresApproval: profile.RequiresApproval,
		Warnings:         append([]string(nil), profile.Warnings...),
	}, nil
}

// SetSandboxes replaces the execution runners used for validation and policy inspection.
func (m *Manager) SetSandboxes(runners ...SandboxRunner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sandboxes = cloneSandboxRunners(runners)
}

// AvailableSandboxes returns the configured execution runners.
func (m *Manager) AvailableSandboxes() []SandboxRunner {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneSandboxRunners(m.sandboxes)
}

func (m *Manager) loadManagedPackages() {
	entries := map[string]RegisteredSkill{}
	_ = filepath.WalkDir(m.packagesRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() || d.Name() != skillPackageMetaFile {
			return nil
		}
		info, err := readPackageInfo(path)
		if err != nil {
			return nil
		}
		root := filepath.Dir(path)
		entry, err := m.loadEntry(root, info, "", "")
		if err != nil {
			return nil
		}
		existing, ok := entries[entry.Registration.ID]
		if !ok || entry.Package.InstalledAt.After(existing.Package.InstalledAt) {
			entries[entry.Registration.ID] = entry
		}
		return nil
	})

	m.mu.Lock()
	defer m.mu.Unlock()
	for id, entry := range entries {
		m.skills[id] = entry
	}
}

func (m *Manager) loadEntry(root string, info core.SkillPackageInfo, name, description string) (RegisteredSkill, error) {
	manifest, err := LoadManifest(root)
	if err != nil {
		return RegisteredSkill{}, err
	}
	if info.SkillID == "" {
		info.SkillID = manifest.ID
	}
	if info.Version == "" {
		info.Version = manifest.Version
	}
	if info.SourceType == "" {
		info.SourceType = "path"
	}
	if info.PackagePath == "" {
		info.PackagePath = root
	}
	if info.InstalledAt.IsZero() {
		info.InstalledAt = time.Now().UTC()
	}
	if name == "" {
		name = manifest.Name
	}
	if name == "" {
		name = manifest.ID
	}
	if description == "" {
		description = manifest.Description
	}

	item := core.SkillRegistration{
		ID:              manifest.ID,
		Name:            name,
		Version:         manifest.Version,
		Description:     description,
		ArchivePath:     root,
		RuntimeType:     manifest.Runtime.Type,
		Effects:         manifest.EffectiveEffects(),
		ApprovalDefault: manifest.Approval.Default,
		SourceType:      info.SourceType,
		Checksum:        info.Checksum,
		InstalledAt:     info.InstalledAt,
		SandboxProfile:  string(manifest.Sandbox.Profile),
		Enabled:         true,
		CreatedAt:       time.Now().UTC(),
	}
	entry := RegisteredSkill{
		Registration: item,
		Manifest:     manifest,
		Root:         root,
		Package:      info,
	}
	if _, err := prepareExecution(entry, map[string]any{}, 0, m.AvailableSandboxes()); err != nil {
		return RegisteredSkill{}, err
	}
	return entry, nil
}

func (m *Manager) store(entry RegisteredSkill) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.skills[entry.Registration.ID]; ok {
		entry.Registration.Enabled = existing.Registration.Enabled
		entry.Registration.CreatedAt = existing.Registration.CreatedAt
	}
	m.skills[entry.Registration.ID] = entry
}

func (m *Manager) resolveRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return abs, nil
	}
	if filepath.Base(abs) == "skill.yaml" {
		return filepath.Dir(abs), nil
	}
	return "", fmt.Errorf("skill path must be a directory or skill.yaml")
}

func extractZipSkill(zipPath, workdir string) (string, Manifest, error) {
	unpackRoot := filepath.Join(workdir, "unpacked")
	if err := os.MkdirAll(unpackRoot, 0o755); err != nil {
		return "", Manifest{}, err
	}
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", Manifest{}, err
	}
	defer reader.Close()

	var totalSize int64
	fileCount := 0
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		fileCount++
		if fileCount > maxZipFiles {
			return "", Manifest{}, fmt.Errorf("zip contains too many files")
		}
		if int64(file.UncompressedSize64) > maxZipFileBytes {
			return "", Manifest{}, fmt.Errorf("zip file %q exceeds per-file size limit", file.Name)
		}
		totalSize += int64(file.UncompressedSize64)
		if totalSize > maxZipTotalBytes {
			return "", Manifest{}, fmt.Errorf("zip exceeds total extracted size limit")
		}

		target, err := safeZipTarget(unpackRoot, file.Name)
		if err != nil {
			return "", Manifest{}, err
		}
		info := file.FileInfo()
		if info.Mode()&os.ModeSymlink != 0 {
			return "", Manifest{}, fmt.Errorf("zip entry %q uses unsupported symlink metadata", file.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", Manifest{}, err
		}
		src, err := file.Open()
		if err != nil {
			return "", Manifest{}, err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			src.Close()
			return "", Manifest{}, err
		}
		written, err := io.Copy(dst, io.LimitReader(src, maxZipFileBytes+1))
		closeErr := dst.Close()
		src.Close()
		if err != nil {
			return "", Manifest{}, err
		}
		if closeErr != nil {
			return "", Manifest{}, closeErr
		}
		if written > maxZipFileBytes {
			return "", Manifest{}, fmt.Errorf("zip file %q exceeds per-file size limit", file.Name)
		}
	}

	skillRoots, err := findSkillRoots(unpackRoot)
	if err != nil {
		return "", Manifest{}, err
	}
	if len(skillRoots) == 0 {
		return "", Manifest{}, fmt.Errorf("zip does not contain skill.yaml")
	}
	if len(skillRoots) > 1 {
		return "", Manifest{}, fmt.Errorf("zip contains multiple skill.yaml manifests")
	}
	manifest, err := LoadManifest(skillRoots[0])
	if err != nil {
		return "", Manifest{}, err
	}
	return skillRoots[0], manifest, nil
}

func findSkillRoots(root string) ([]string, error) {
	items := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() || d.Name() != "skill.yaml" {
			return nil
		}
		items = append(items, filepath.Dir(path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(items)
	return items, nil
}

func safeZipTarget(root, name string) (string, error) {
	cleaned := filepath.Clean(strings.TrimPrefix(name, "/"))
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("zip entry %q is invalid", name)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("zip entry %q escapes the target directory", name)
	}
	target := filepath.Join(root, cleaned)
	if !pathWithinBase(target, root) {
		return "", fmt.Errorf("zip entry %q escapes the target directory", name)
	}
	return target, nil
}

func localPackageInfo(root string) core.SkillPackageInfo {
	return core.SkillPackageInfo{
		SourceType:  "path",
		PackagePath: root,
		InstalledAt: time.Now().UTC(),
	}
}

func writePackageInfo(root string, info core.SkillPackageInfo) error {
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, skillPackageMetaFile), append(raw, '\n'), 0o644)
}

func readPackageInfo(path string) (core.SkillPackageInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return core.SkillPackageInfo{}, err
	}
	var info core.SkillPackageInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return core.SkillPackageInfo{}, err
	}
	return info, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), nil
}
