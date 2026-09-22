package provisioning

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

var ErrRequiresCycle = errors.New("provisioning: requires cycle detected")

var ErrMissingDependency = errors.New("provisioning: missing requires dependency")

type ManifestLookup func(ctx context.Context, name, version string) (PackageManifest, error)

func ResolveRequires(ctx context.Context, roots []PackageManifest, lookup ManifestLookup) ([]PackageManifest, error) {
	resolved := make(map[string]PackageManifest, len(roots))
	visiting := make(map[string]bool)
	order := make([]string, 0, len(roots))

	var visit func(m PackageManifest) error
	visit = func(m PackageManifest) error {
		key := m.RequireKey()
		if _, done := resolved[key]; done {
			return nil
		}
		if visiting[key] {
			return fmt.Errorf("%w: %s", ErrRequiresCycle, key)
		}
		visiting[key] = true

		for _, ref := range m.Requires {
			reqType, reqName, reqVersion, err := ParseRequireRef(ref)
			if err != nil {

				delete(visiting, key)
				return fmt.Errorf("provisioning: %s has invalid requires entry %q: %w", key, ref, err)
			}
			dep, err := lookup(ctx, reqName, reqVersion)
			if err != nil {
				delete(visiting, key)
				return fmt.Errorf("%w: %s requires %s: %v", ErrMissingDependency, key, ref, err)
			}
			if dep.Type != reqType {
				delete(visiting, key)
				return fmt.Errorf("provisioning: %s requires %s but resolved package has type %q", key, ref, dep.Type)
			}
			if err := visit(dep); err != nil {
				delete(visiting, key)
				return err
			}
		}

		delete(visiting, key)
		resolved[key] = m
		order = append(order, key)
		return nil
	}

	for _, root := range roots {
		if err := visit(root); err != nil {
			return nil, err
		}
	}

	out := make([]PackageManifest, 0, len(order))
	for _, key := range order {
		out = append(out, resolved[key])
	}
	return out, nil
}

type UnavailablePackage struct {
	Key    string
	Reason string
}

func ResolveRequiresPartial(ctx context.Context, roots []PackageManifest, lookup ManifestLookup) (resolved []PackageManifest, unavailable []UnavailablePackage, err error) {
	store := make(map[string]PackageManifest, len(roots))
	order := make([]string, 0, len(roots))

	for _, root := range roots {
		key := root.RequireKey()
		if _, done := store[key]; done {
			continue
		}
		reason, cerr := resolveClosureOrReason(ctx, root, lookup, store, &order)
		if cerr != nil {
			return nil, nil, cerr
		}
		if reason != "" {
			unavailable = append(unavailable, UnavailablePackage{Key: key, Reason: reason})
		}
	}

	resolved = make([]PackageManifest, 0, len(order))
	for _, key := range order {
		resolved = append(resolved, store[key])
	}
	return resolved, unavailable, nil
}

func resolveClosureOrReason(ctx context.Context, root PackageManifest, lookup ManifestLookup, store map[string]PackageManifest, order *[]string) (reason string, err error) {
	visiting := make(map[string]bool)
	addedFrom := len(*order)

	var visit func(m PackageManifest) (string, error)
	visit = func(m PackageManifest) (string, error) {
		key := m.RequireKey()
		if _, done := store[key]; done {
			return "", nil
		}
		if visiting[key] {
			return "", fmt.Errorf("%w: %s", ErrRequiresCycle, key)
		}
		visiting[key] = true
		defer delete(visiting, key)

		for _, ref := range m.Requires {
			reqType, reqName, reqVersion, perr := ParseRequireRef(ref)
			if perr != nil {
				return "", fmt.Errorf("provisioning: %s has invalid requires entry %q: %w", key, ref, perr)
			}
			dep, lerr := lookup(ctx, reqName, reqVersion)
			if lerr != nil {
				if errors.Is(lerr, ErrPackageNotFound) {
					return fmt.Sprintf("dependency unavailable: %s requires %s@%s", m.Name, reqName, reqVersion), nil
				}

				return "", fmt.Errorf("provisioning: %s requires %s: %w", key, ref, lerr)
			}
			if dep.Type != reqType {
				return "", fmt.Errorf("provisioning: %s requires %s but resolved package has type %q", key, ref, dep.Type)
			}
			if depReason, err := visit(dep); err != nil {
				return "", err
			} else if depReason != "" {
				return depReason, nil
			}
		}

		store[key] = m
		*order = append(*order, key)
		return "", nil
	}

	reason, err = visit(root)
	if err != nil || reason != "" {

		for _, k := range (*order)[addedFrom:] {
			delete(store, k)
		}
		*order = (*order)[:addedFrom]
	}
	return reason, err
}

func FilterByPlatform(manifests []PackageManifest, platform string) []PackageManifest {
	out := make([]PackageManifest, 0, len(manifests))
	for _, m := range manifests {
		if m.MatchesPlatform(platform) {
			out = append(out, m)
		}
	}
	return out
}

func PlatformsOf(manifests []PackageManifest) []string {
	seen := map[string]bool{}
	for _, m := range manifests {
		seen[m.Platform] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
