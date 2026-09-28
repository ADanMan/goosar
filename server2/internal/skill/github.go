package skill

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrUpstreamUnavailable — источник вернул ошибку/недоступен (502/503 у
// вызывающего HTTP-кода в зависимости от природы сбоя).
var ErrUpstreamUnavailable = errors.New("skill: источник недоступен")

var githubHTTPClient = &http.Client{Timeout: ImportTimeoutSecs * time.Second}

type githubContentEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"` // "file" | "dir"
	Size        int64  `json:"size"`
	Content     string `json:"content"`
	Encoding    string `json:"encoding"`
	DownloadURL string `json:"download_url"`
}

// parseGitHubURL извлекает owner/repo/ref/subpath из ссылок вида
// https://github.com/{owner}/{repo}[/tree/{ref}[/{path}]].
func parseGitHubURL(rawURL string) (owner, repo, ref, subpath string, err error) {
	u, perr := url.Parse(rawURL)
	if perr != nil {
		return "", "", "", "", perr
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", "", "", fmt.Errorf("skill: некорректная ссылка github.com")
	}
	owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
	ref = "HEAD"
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		ref = parts[3]
		if len(parts) > 4 {
			subpath = strings.Join(parts[4:], "/")
		}
	}
	return owner, repo, ref, subpath, nil
}

// fromGitHub получает дерево файлов через GitHub REST "contents" API
// (публичные репозитории, без токена — deploy-level GITHUB_APP_* здесь не
// используется намеренно: это персональный импорт участника, не интеграция
// деплоя). Рекурсивно обходит каталоги, применяя те же лимиты, что ZIP.
func fromGitHub(ctx context.Context, rawURL string) (name, description, content string, files []FileInput, err error) {
	owner, repo, ref, subpath, perr := parseGitHubURL(rawURL)
	if perr != nil {
		return "", "", "", nil, perr
	}
	ctx, cancel := context.WithTimeout(ctx, ImportTimeoutSecs*time.Second)
	defer cancel()

	entries, walkErr := walkGitHub(ctx, owner, repo, ref, subpath)
	if walkErr != nil {
		return "", "", "", nil, walkErr
	}
	skillMD, fileInputs, extractErr := extractBundle(entries)
	if extractErr != nil {
		return "", "", "", nil, extractErr
	}
	fmName, fmDesc, body := parseFrontmatter(skillMD)
	if fmName == "" {
		fmName = repo
	}
	return fmName, fmDesc, body, fileInputs, nil
}

func walkGitHub(ctx context.Context, owner, repo, ref, subpath string) ([]bundleEntry, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		url.PathEscape(owner), url.PathEscape(repo), subpath, url.QueryEscape(ref))
	list, err := getGitHubJSON(ctx, apiURL)
	if err != nil {
		return nil, err
	}
	var out []bundleEntry
	for _, e := range list {
		switch e.Type {
		case "file":
			if isBinaryPath(e.Path) || isLicensePath(e.Path) {
				continue
			}
			if e.Size > MaxImportFileBytes {
				return nil, ErrTooLarge
			}
			body, berr := fetchGitHubFileBody(ctx, e)
			if berr != nil {
				return nil, berr
			}
			out = append(out, bundleEntry{Path: strings.TrimPrefix(e.Path, subpath+"/"), Content: body})
		case "dir":
			if len(out) > MaxImportFiles {
				return nil, ErrTooManyFiles
			}
			sub, serr := walkGitHub(ctx, owner, repo, ref, e.Path)
			if serr != nil {
				return nil, serr
			}
			out = append(out, sub...)
		}
		if len(out) > MaxImportFiles {
			return nil, ErrTooManyFiles
		}
	}
	return out, nil
}

func getGitHubJSON(ctx context.Context, apiURL string) ([]githubContentEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("skill: путь не найден в репозитории")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: github api вернул %d", ErrUpstreamUnavailable, resp.StatusCode)
	}
	// Один файл по указанному пути отдаётся объектом, не массивом.
	var single githubContentEntry
	if err := json.Unmarshal(body, &single); err == nil && single.Type == "file" {
		return []githubContentEntry{single}, nil
	}
	var list []githubContentEntry
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("%w: неожиданный ответ github api", ErrUpstreamUnavailable)
	}
	return list, nil
}

func fetchGitHubFileBody(ctx context.Context, e githubContentEntry) (string, error) {
	if e.Encoding == "base64" && e.Content != "" {
		raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(e.Content, "\n", ""))
		if err != nil {
			return "", fmt.Errorf("skill: декодирование содержимого github: %w", err)
		}
		return string(raw), nil
	}
	if e.DownloadURL == "" {
		return "", fmt.Errorf("%w: файл без содержимого и download_url", ErrUpstreamUnavailable)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.DownloadURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: github вернул %d при скачивании файла", ErrUpstreamUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxImportFileBytes+1))
	if err != nil {
		return "", err
	}
	return string(body), nil
}
