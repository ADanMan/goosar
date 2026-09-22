package mcplibrary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/util/secretbox"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type Outcome string

const (
	OutcomeCreated       Outcome = "created"
	OutcomeUpdated       Outcome = "updated"
	OutcomeUpToDate      Outcome = "up-to-date"
	OutcomeSkippedManual Outcome = "skipped (hand-edited)"
	OutcomeDryRun        Outcome = "dry-run"
)

type Result struct {
	Name    string
	Outcome Outcome

	Detail string
}

var ErrNoSecretKey = errors.New("mcp-library seed requires GOOSAR_MCP_SECRET_KEY to be configured")

func Seed(ctx context.Context, queries *db.Queries, box *secretbox.Box, specs []Spec, dryRun bool, out io.Writer) ([]Result, error) {
	if box == nil {
		return nil, ErrNoSecretKey
	}
	results := make([]Result, 0, len(specs))
	for _, spec := range specs {
		result, err := seedOne(ctx, queries, box, spec, dryRun)
		if err != nil {
			return results, fmt.Errorf("%s: %w", spec.Name, err)
		}
		results = append(results, result)
		if out != nil {
			fmt.Fprintf(out, "%-12s %s%s\n", spec.Name, result.Outcome, detailSuffix(result.Detail))
		}
	}
	return results, nil
}

func detailSuffix(detail string) string {
	if detail == "" {
		return ""
	}
	return ": " + detail
}

func seedOne(ctx context.Context, queries *db.Queries, box *secretbox.Box, spec Spec, dryRun bool) (Result, error) {
	configJSON, err := MarshalConfig(spec.Config)
	if err != nil {
		return Result{}, fmt.Errorf("marshal config: %w", err)
	}
	schemaJSON, err := json.Marshal(normalizeCredentialSchema(spec.CredentialSchema))
	if err != nil {
		return Result{}, fmt.Errorf("marshal credential_schema: %w", err)
	}

	existing, err := queries.GetDeploymentMcpServerByName(ctx, spec.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		if dryRun {
			return Result{Name: spec.Name, Outcome: OutcomeDryRun, Detail: "would create"}, nil
		}
		sealed, err := handler.SealConfigDocumentWithBox(box, configJSON)
		if err != nil {
			return Result{}, fmt.Errorf("seal config: %w", err)
		}
		if _, err := queries.CreateDeploymentMcpServer(ctx, db.CreateDeploymentMcpServerParams{
			Name:             spec.Name,
			Config:           sealed,
			Transport:        spec.Transport,
			CredentialSchema: schemaJSON,
			CreatedBy:        pgtype.UUID{},
		}); err != nil {
			return Result{}, fmt.Errorf("create: %w", err)
		}
		return Result{Name: spec.Name, Outcome: OutcomeCreated}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("look up existing record: %w", err)
	}

	if existing.Transport != spec.Transport {
		return Result{
			Name: spec.Name, Outcome: OutcomeSkippedManual,
			Detail: fmt.Sprintf("transport is %q, seeding would set %q", existing.Transport, spec.Transport),
		}, nil
	}
	if !credentialSchemaMatches(spec, existing.CredentialSchema) {
		return Result{
			Name: spec.Name, Outcome: OutcomeSkippedManual,
			Detail: "credential_schema was changed after seeding",
		}, nil
	}
	openConfig, err := handler.OpenConfigDocumentWithBox(box, existing.Config)
	if err != nil {
		return Result{
			Name: spec.Name, Outcome: OutcomeSkippedManual,
			Detail: fmt.Sprintf("stored config could not be opened: %v", err),
		}, nil
	}
	if !configMatchesModuloAddress(spec, openConfig) {
		return Result{
			Name: spec.Name, Outcome: OutcomeSkippedManual,
			Detail: "config was changed after seeding (fields other than the address differ)",
		}, nil
	}

	if string(openConfig) == string(configJSON) {
		return Result{Name: spec.Name, Outcome: OutcomeUpToDate}, nil
	}
	if dryRun {
		return Result{Name: spec.Name, Outcome: OutcomeDryRun, Detail: "would update address"}, nil
	}
	sealed, err := handler.SealConfigDocumentWithBox(box, configJSON)
	if err != nil {
		return Result{}, fmt.Errorf("seal config: %w", err)
	}

	if _, err := queries.UpdateDeploymentMcpServer(ctx, db.UpdateDeploymentMcpServerParams{
		ID:        existing.ID,
		Config:    sealed,
		Transport: pgtype.Text{String: spec.Transport, Valid: true},
	}); err != nil {
		return Result{}, fmt.Errorf("update: %w", err)
	}
	return Result{Name: spec.Name, Outcome: OutcomeUpdated}, nil
}
