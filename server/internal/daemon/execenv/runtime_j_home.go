package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

const hermesTaskLocalStateMarker = ".goosar-task-local-state-v1"

var hermesOverriddenEntries = map[string]struct{}{
	"skills":                   {},
	"config.yaml":              {},
	"memories":                 {},
	"active_profile":           {},
	"profiles":                 {},
	".env":                     {},
	hermesTaskLocalStateMarker: {},
}

func isHermesOverlayOwnedEntry(name string) bool {
	if _, owned := hermesOverriddenEntries[name]; owned {
		return true
	}
	return isHermesTaskLocalStateEntry(name)
}

func isHermesTaskLocalStateEntry(name string) bool {
	return name == "state.db" || strings.HasPrefix(name, "state.db-")
}

func platformDefaultHermesHome() string {
	la, _ := os.LookupEnv("LOCALAPPDATA")
	home, _ := os.UserHomeDir()
	return platformDefaultHermesHomeFor(runtime.GOOS, la, home)
}

func platformDefaultHermesHomeFor(goos, localAppData, userHome string) string {
	spec := runtimeTaskHomeSpec(runtimeCodeJ)
	if goos == "windows" && spec.WindowsAppDataSubpath != "" {
		base := strings.TrimSpace(localAppData)
		if base == "" && userHome != "" {
			base = filepath.Join(userHome, "AppData", "Local")
		}
		if base != "" {
			return filepath.Join(base, filepath.FromSlash(spec.WindowsAppDataSubpath))
		}
	}
	posix := filepath.FromSlash(spec.PosixSubpath)
	if userHome != "" {
		return filepath.Join(userHome, posix)
	}
	return filepath.Join(os.TempDir(), posix)
}

var hermesProfileNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

var hermesReservedProfileNames = map[string]struct{}{
	"hermes": {}, "test": {}, "tmp": {}, "root": {}, "sudo": {},
}

type HermesProfileResolution struct {
	SourceHome string

	MustExist bool

	Err error
}

func ResolveHermesProfile(customEnvHome, name string, found, inline bool) HermesProfileResolution {
	base := strings.TrimSpace(customEnvHome)
	if base == "" {
		base = strings.TrimSpace(os.Getenv(RuntimeJHomeEnv))
	}
	if base == "" {
		base = platformDefaultHermesHome()
	}
	if abs, err := filepath.Abs(base); err == nil {
		base = abs
	}
	root := hermesRootFromHome(base)

	profile := name
	if !found {

		if base != "" && filepath.Base(filepath.Dir(base)) == "profiles" {
			return HermesProfileResolution{SourceHome: base, MustExist: true}
		}

		profile = readHermesActiveProfile(root)
		if profile == "" {
			return HermesProfileResolution{SourceHome: base}
		}
	}

	home, mustExist, err := hermesProfileDir(root, profile)
	if err != nil {
		return HermesProfileResolution{Err: err}
	}
	return HermesProfileResolution{SourceHome: home, MustExist: mustExist}
}

func hermesRootFromHome(base string) string {
	return hermesRootFromHomeFor(base, platformDefaultHermesHome())
}

func hermesRootFromHomeFor(base, native string) string {
	if base == "" {
		return native
	}
	if isPathUnder(resolvePathBestEffort(native), resolvePathBestEffort(base)) {
		return native
	}
	if filepath.Base(filepath.Dir(base)) == "profiles" {
		return filepath.Dir(filepath.Dir(base))
	}
	return base
}

func resolvePathBestEffort(p string) string {
	if p == "" {
		return p
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	dir := p
	var tail []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		tail = append([]string{filepath.Base(dir)}, tail...)
		dir = parent
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{resolved}, tail...)...)
		}
	}
}

func isPathUnder(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func readHermesActiveProfile(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "active_profile"))
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(data))
	if name == "default" {
		return ""
	}
	return name
}

func hermesProfileDir(root, name string) (home string, mustExist bool, err error) {
	stripped := strings.TrimSpace(name)
	if stripped == "" {
		return "", false, fmt.Errorf("hermes profile name cannot be empty")
	}
	var canon string
	if strings.EqualFold(stripped, "default") {
		canon = "default"
	} else {
		canon = strings.ToLower(stripped)
	}
	if canon == "default" {
		return root, false, nil
	}
	if !hermesProfileNameRe.MatchString(canon) {
		return "", false, fmt.Errorf("invalid hermes profile name %q", canon)
	}
	if _, reserved := hermesReservedProfileNames[canon]; reserved {
		return "", false, fmt.Errorf("hermes profile name %q is reserved", canon)
	}
	return filepath.Join(root, "profiles", canon), true, nil
}

func prepareHermesHome(hermesHome, sourceHome string, sourceMustExist bool, workspaceSkills []SkillContextForEnv, env map[string]string, logger *slog.Logger) error {
	sharedHome := strings.TrimSpace(sourceHome)
	if sharedHome == "" {
		sharedHome = platformDefaultHermesHome()
	}
	if sourceMustExist {
		if fi, err := os.Stat(sharedHome); err != nil || !fi.IsDir() {
			return fmt.Errorf("hermes profile home %q not found (create it with `hermes profile create`)", sharedHome)
		}
	}

	if err := os.MkdirAll(hermesHome, 0o700); err != nil {
		return fmt.Errorf("create hermes-home dir: %w", err)
	}

	if err := os.Chmod(hermesHome, 0o700); err != nil {
		return fmt.Errorf("chmod hermes-home dir: %w", err)
	}
	if err := prepareHermesTaskLocalState(hermesHome); err != nil {
		return fmt.Errorf("prepare task-local state: %w", err)
	}

	if err := os.MkdirAll(filepath.Join(hermesHome, "memories"), 0o700); err != nil {
		return fmt.Errorf("create task memories dir: %w", err)
	}

	if err := mirrorSharedHermesHome(sharedHome, hermesHome, logger); err != nil {
		return fmt.Errorf("mirror shared hermes home: %w", err)
	}
	if err := writeDerivedHermesConfig(sharedHome, hermesHome, env, logger); err != nil {
		return fmt.Errorf("derive hermes config: %w", err)
	}
	if err := writeDerivedHermesEnv(sharedHome, hermesHome); err != nil {
		return fmt.Errorf("derive hermes .env: %w", err)
	}
	return writeHermesBoundSkills(hermesHome, workspaceSkills, logger)
}

func writeDerivedHermesEnv(sharedHome, hermesHome string) error {
	dst := filepath.Join(hermesHome, ".env")

	var body []byte
	src, err := os.ReadFile(filepath.Join(sharedHome, ".env"))
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read shared .env: %w", err)
		}
	} else {
		body = stripDotenvAssignment(src, RuntimeJHomeEnv)
	}

	var buf strings.Builder
	if len(body) > 0 {
		buf.Write(body)
		if body[len(body)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}

	fmt.Fprintf(&buf, "%s='%s'\n", RuntimeJHomeEnv, hermesHome)

	return writeFileAtomic(dst, []byte(buf.String()), 0o600)
}

func stripDotenvAssignment(content []byte, key string) []byte {
	lines := strings.Split(string(content), "\n")
	out := lines[:0]
	for _, line := range lines {
		if dotenvLineKey(line) == key {
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

func dotenvLineKey(line string) string {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return ""
	}
	if rest := strings.TrimPrefix(s, "export"); rest != s && rest != "" &&
		(rest[0] == ' ' || rest[0] == '\t') {
		s = strings.TrimSpace(rest)
	}
	eq := strings.IndexByte(s, '=')
	if eq <= 0 {
		return ""
	}
	return strings.TrimSpace(s[:eq])
}

func mirrorSharedHermesHome(sharedHome, hermesHome string, logger *slog.Logger) error {
	entries, err := os.ReadDir(sharedHome)
	if err != nil {
		if os.IsNotExist(err) {

			return reconcileMirroredEntries(hermesHome, nil)
		}
		return fmt.Errorf("read shared home: %w", err)
	}
	mirrored := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if isHermesOverlayOwnedEntry(name) {
			continue
		}
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(hermesHome, name)
		if err := linkSharedHermesEntry(src, dst); err != nil {
			return fmt.Errorf("mirror %s: %w", name, err)
		}
		mirrored[name] = struct{}{}
	}
	return reconcileMirroredEntries(hermesHome, mirrored)
}

func reconcileMirroredEntries(hermesHome string, mirrored map[string]struct{}) error {
	entries, err := os.ReadDir(hermesHome)
	if err != nil {
		return fmt.Errorf("read overlay home: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if isHermesOverlayOwnedEntry(name) {
			continue
		}
		if _, keep := mirrored[name]; keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(hermesHome, name)); err != nil {
			return fmt.Errorf("reconcile stale %s: %w", name, err)
		}
	}
	return nil
}

func prepareHermesTaskLocalState(hermesHome string) error {
	marker := filepath.Join(hermesHome, hermesTaskLocalStateMarker)
	if fi, err := os.Lstat(marker); err == nil {
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("state marker is not a regular file: %s", marker)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat state marker: %w", err)
	}

	entries, err := os.ReadDir(hermesHome)
	if err != nil {
		return fmt.Errorf("read overlay home: %w", err)
	}
	for _, entry := range entries {
		if !isHermesTaskLocalStateEntry(entry.Name()) {
			continue
		}
		path := filepath.Join(hermesHome, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove legacy task state %s: %w", path, err)
		}
	}
	return writeFileAtomic(marker, []byte("task-local Hermes state\n"), 0o600)
}

func linkSharedHermesEntry(src, dst string) error {
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(dst); err == nil && target == src {
				return nil
			}
		}
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("remove stale %s: %w", dst, err)
		}
	}

	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if info.IsDir() {
		return createDirLink(src, dst)
	}
	return createFileLink(src, dst)
}

func writeDerivedHermesConfig(sharedHome, hermesHome string, env map[string]string, logger *slog.Logger) error {
	srcConfig := filepath.Join(sharedHome, "config.yaml")
	dstConfig := filepath.Join(hermesHome, "config.yaml")

	data, err := os.ReadFile(srcConfig)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read shared config: %w", err)
		}
		doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
		if err := setHermesExternalDirs(doc, computeHermesExternalDirs(sharedHome, nil, env)); err != nil {
			return err
		}
		return marshalYAMLToFile(doc, dstConfig)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		logger.Warn("execenv: hermes-home config parse failed; copying verbatim", "error", err)
		return writeFileAtomic(dstConfig, data, 0o600)
	}
	dirs := computeHermesExternalDirs(sharedHome, existingHermesExternalDirs(&doc), env)
	if err := setHermesExternalDirs(&doc, dirs); err != nil {
		logger.Warn("execenv: hermes-home set external_dirs failed; copying verbatim", "error", err)
		return writeFileAtomic(dstConfig, data, 0o600)
	}

	disableHermesMemoryProvider(&doc)
	return marshalYAMLToFile(&doc, dstConfig)
}

func disableHermesMemoryProvider(doc *yaml.Node) {
	top := yamlDocumentRoot(doc)
	if top == nil {
		return
	}
	memory := yamlMapValue(top, "memory")
	if memory == nil || memory.Kind != yaml.MappingNode {
		memory = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlSetMapValue(top, "memory", memory)
	}
	yamlSetMapValue(memory, "provider", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""})
}

func computeHermesExternalDirs(sharedHome string, existing []string, env map[string]string) []string {
	expand := func(s string) string {
		return os.Expand(s, func(k string) string {
			if v, ok := env[k]; ok {
				return v
			}
			if v, ok := os.LookupEnv(k); ok {
				return v
			}
			return "${" + k + "}"
		})
	}

	out := make([]string, 0, len(existing)+1)
	seen := make(map[string]struct{}, len(existing)+1)
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for _, raw := range existing {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		entry = strings.TrimSpace(expand(entry))
		if entry == "" {
			continue
		}

		if strings.Contains(entry, "${") {
			add(entry)
			continue
		}
		if entry == "~" || strings.HasPrefix(entry, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				entry = filepath.Join(home, strings.TrimPrefix(entry, "~"))
			}
		}
		if !filepath.IsAbs(entry) {
			entry = filepath.Join(sharedHome, entry)
		}
		add(filepath.Clean(entry))
	}

	add(filepath.Join(sharedHome, "skills"))
	return out
}

func writeHermesBoundSkills(hermesHome string, workspaceSkills []SkillContextForEnv, logger *slog.Logger) error {
	skillsDir := filepath.Join(hermesHome, "skills")
	if err := os.RemoveAll(skillsDir); err != nil {
		return fmt.Errorf("clear hermes skills dir: %w", err)
	}
	if len(workspaceSkills) == 0 {

		return os.MkdirAll(skillsDir, 0o700)
	}

	return writeSkillFiles(skillsDir, workspaceSkills, nil)
}

func existingHermesExternalDirs(doc *yaml.Node) []string {
	top := yamlDocumentRoot(doc)
	if top == nil {
		return nil
	}
	skills := yamlMapValue(top, "skills")
	ed := yamlMapValue(skills, "external_dirs")
	if ed == nil {
		return nil
	}
	if ed.Kind == yaml.ScalarNode {
		return []string{ed.Value}
	}
	if ed.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(ed.Content))
	for _, c := range ed.Content {
		if c.Kind == yaml.ScalarNode {
			out = append(out, c.Value)
		}
	}
	return out
}

func setHermesExternalDirs(doc *yaml.Node, dirs []string) error {
	top := yamlDocumentRoot(doc)
	if top == nil {
		return fmt.Errorf("hermes config: unexpected root node")
	}
	skills := yamlMapValue(top, "skills")
	if skills == nil || skills.Kind != yaml.MappingNode {
		skills = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlSetMapValue(top, "skills", skills)
	}
	yamlSetMapValue(skills, "external_dirs", yamlStringSeq(dirs))
	return nil
}

func yamlDocumentRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	node := doc
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

func yamlMapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func yamlSetMapValue(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		val,
	)
}

func yamlStringSeq(vals []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, v := range vals {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
	}
	return seq
}

func marshalYAMLToFile(doc *yaml.Node, dst string) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal hermes config: %w", err)
	}
	return writeFileAtomic(dst, out, 0o600)
}

func writeFileAtomic(dst string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".hermes-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", dst, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp for %s: %w", dst, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp for %s: %w", dst, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", dst, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("rename temp to %s: %w", dst, err)
	}
	return nil
}
