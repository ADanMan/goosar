package handler

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	skillpkg "github.com/adanman/goosar/server/internal/skill"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type skillCreateInput struct {
	WorkspaceID pgtype.UUID
	CreatorID   pgtype.UUID
	Name        string
	Description string
	Content     string
	Config      any
	Files       []CreateSkillFileRequest
}

func createSkillWithFilesInTx(ctx context.Context, qtx *db.Queries, input skillCreateInput) (SkillWithFilesResponse, error) {
	config, err := json.Marshal(input.Config)
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	if input.Config == nil {
		config = []byte("{}")
	}

	skill, err := qtx.CreateSkill(ctx, db.CreateSkillParams{
		WorkspaceID: input.WorkspaceID,
		Name:        sanitizeNullBytes(input.Name),
		Description: sanitizeNullBytes(input.Description),
		Content:     sanitizeNullBytes(input.Content),
		Config:      config,
		CreatedBy:   input.CreatorID,
	})
	if err != nil {
		return SkillWithFilesResponse{}, err
	}

	fileResps := make([]SkillFileResponse, 0, len(input.Files))
	for _, f := range input.Files {

		if skillpkg.IsReservedContentPath(f.Path) {
			continue
		}
		sf, err := qtx.UpsertSkillFile(ctx, db.UpsertSkillFileParams{
			SkillID: skill.ID,
			Path:    sanitizeNullBytes(f.Path),
			Content: sanitizeNullBytes(f.Content),
		})
		if err != nil {
			return SkillWithFilesResponse{}, err
		}
		fileResps = append(fileResps, skillFileToResponse(sf))
	}

	return SkillWithFilesResponse{
		SkillResponse: skillToResponse(skill),
		Files:         fileResps,
	}, nil
}

func (h *Handler) createSkillWithFiles(ctx context.Context, input skillCreateInput) (SkillWithFilesResponse, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)

	result, err := createSkillWithFilesInTx(ctx, qtx, input)
	if err != nil {
		return SkillWithFilesResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return SkillWithFilesResponse{}, err
	}

	return result, nil
}

var (
	errSkillOverwriteNotFound     = errors.New("target skill not found")
	errSkillOverwriteForbidden    = errors.New("not permitted to overwrite target skill")
	errSkillOverwriteNameMismatch = errors.New("target skill name does not match the imported skill")
)

type skillOverwriteInput struct {
	WorkspaceID   pgtype.UUID
	TargetSkillID pgtype.UUID
	UserID        string

	ExpectedName string
	Description  string
	Content      string
	Config       any
	Files        []CreateSkillFileRequest
}

func (h *Handler) overwriteSkillWithFiles(ctx context.Context, input skillOverwriteInput) (SkillWithFilesResponse, error) {
	config, err := json.Marshal(input.Config)
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	if input.Config == nil {
		config = []byte("{}")
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)

	existing, err := qtx.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{
		ID:          input.TargetSkillID,
		WorkspaceID: input.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SkillWithFilesResponse{}, errSkillOverwriteNotFound
		}
		return SkillWithFilesResponse{}, err
	}
	if !canOverwriteSkillByLocalImport(input.UserID, existing) {
		return SkillWithFilesResponse{}, errSkillOverwriteForbidden
	}

	if input.ExpectedName != "" && existing.Name != input.ExpectedName {
		return SkillWithFilesResponse{}, errSkillOverwriteNameMismatch
	}

	skill, err := qtx.UpdateSkill(ctx, db.UpdateSkillParams{
		ID:          existing.ID,
		Description: pgtype.Text{String: sanitizeNullBytes(input.Description), Valid: true},
		Content:     pgtype.Text{String: sanitizeNullBytes(input.Content), Valid: true},
		Config:      config,
	})
	if err != nil {

		if errors.Is(err, pgx.ErrNoRows) {
			return SkillWithFilesResponse{}, errSkillOverwriteNotFound
		}
		return SkillWithFilesResponse{}, err
	}

	if err := qtx.DeleteSkillFilesBySkill(ctx, skill.ID); err != nil {
		return SkillWithFilesResponse{}, err
	}
	fileResps := make([]SkillFileResponse, 0, len(input.Files))
	for _, f := range input.Files {
		sf, err := qtx.UpsertSkillFile(ctx, db.UpsertSkillFileParams{
			SkillID: skill.ID,
			Path:    sanitizeNullBytes(f.Path),
			Content: sanitizeNullBytes(f.Content),
		})
		if err != nil {
			return SkillWithFilesResponse{}, err
		}
		fileResps = append(fileResps, skillFileToResponse(sf))
	}

	if err := tx.Commit(ctx); err != nil {
		return SkillWithFilesResponse{}, err
	}

	return SkillWithFilesResponse{
		SkillResponse: skillToResponse(skill),
		Files:         fileResps,
	}, nil
}
