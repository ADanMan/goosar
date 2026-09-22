package ghsnapshot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

const prSnapshotQuery = `query($owner:String!,$repo:String!,$number:Int!,$cursor:String){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$number){
      headRefOid
      mergeable
      mergeStateStatus
      commits(last:1){nodes{commit{
        statusCheckRollup{
          state
          contexts(first:100,after:$cursor){
            pageInfo{hasNextPage endCursor}
            nodes{
              __typename
              ... on CheckRun{name status conclusion detailsUrl}
              ... on StatusContext{context state targetUrl}
            }
          }
        }
      }}}
    }
  }
}`

type CheckContext struct {
	Name string

	Status string

	Conclusion      string
	DetailsURL      string
	IsStatusContext bool
}

type PRSnapshot struct {
	HeadSHA          string
	Mergeable        string
	MergeStateStatus string

	RollupState string

	HasChecks bool
	Contexts  []CheckContext
}

func (s *PRSnapshot) Decided() bool {
	if s.Mergeable == "UNKNOWN" || s.Mergeable == "" {
		return false
	}
	if s.HasChecks {
		switch s.RollupState {
		case "PENDING", "EXPECTED", "":
			return false
		}
	}
	for _, c := range s.Contexts {
		if c.Status != "completed" {
			return false
		}
	}
	return true
}

type graphqlRollup struct {
	State    string `json:"state"`
	Contexts struct {
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []json.RawMessage `json:"nodes"`
	} `json:"contexts"`
}

type graphqlPullRequest struct {
	HeadRefOid       string `json:"headRefOid"`
	Mergeable        string `json:"mergeable"`
	MergeStateStatus string `json:"mergeStateStatus"`
	Commits          struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *graphqlRollup `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

func (pr *graphqlPullRequest) rollup() *graphqlRollup {
	if len(pr.Commits.Nodes) == 0 {
		return nil
	}
	return pr.Commits.Nodes[0].Commit.StatusCheckRollup
}

type graphqlPRData struct {
	Repository struct {
		PullRequest *graphqlPullRequest `json:"pullRequest"`
	} `json:"repository"`
}

const maxSnapshotContextPages = 100

func FetchPRSnapshot(ctx context.Context, c *Client, installationID int64, owner, repo string, number int32) (*PRSnapshot, error) {
	if !c.Enabled() {
		return nil, errors.New("ghsnapshot: client not configured")
	}
	snap := &PRSnapshot{}
	cursor := ""

	for page := 0; page < maxSnapshotContextPages; page++ {
		vars := map[string]any{"owner": owner, "repo": repo, "number": number}
		if cursor != "" {
			vars["cursor"] = cursor
		} else {
			vars["cursor"] = nil
		}
		data, err := c.graphQL(ctx, installationID, prSnapshotQuery, vars)
		if err != nil {
			return nil, err
		}
		var parsed graphqlPRData
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, errors.New("ghsnapshot: malformed pull request data")
		}
		pr := parsed.Repository.PullRequest
		if pr == nil {
			return nil, errors.New("ghsnapshot: pull request not found")
		}
		if page == 0 {
			snap.HeadSHA = pr.HeadRefOid
			snap.Mergeable = pr.Mergeable
			snap.MergeStateStatus = pr.MergeStateStatus
		} else if pr.HeadRefOid != snap.HeadSHA {

			return nil, errors.New("ghsnapshot: pull request head changed during pagination")
		}
		rollup := pr.rollup()
		if rollup == nil {

			if page > 0 {
				return nil, errors.New("ghsnapshot: check rollup changed during pagination")
			}
			return snap, nil
		}
		snap.HasChecks = true
		snap.RollupState = rollup.State
		for _, raw := range rollup.Contexts.Nodes {
			if cc, ok := normalizeNode(raw); ok {
				snap.Contexts = append(snap.Contexts, cc)
			}
		}
		if !rollup.Contexts.PageInfo.HasNextPage {
			return snap, nil
		}
		nextCursor := rollup.Contexts.PageInfo.EndCursor
		if nextCursor == "" || nextCursor == cursor {
			return nil, errors.New("ghsnapshot: invalid check-context pagination cursor")
		}
		if page == maxSnapshotContextPages-1 {
			return nil, errors.New("ghsnapshot: check-context pagination exceeds page limit")
		}
		cursor = nextCursor
	}
	return nil, errors.New("ghsnapshot: check-context pagination exceeds page limit")
}

func normalizeNode(raw json.RawMessage) (CheckContext, bool) {
	var probe struct {
		Typename string `json:"__typename"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return CheckContext{}, false
	}
	switch probe.Typename {
	case "CheckRun":
		var n struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			DetailsURL string `json:"detailsUrl"`
		}
		if err := json.Unmarshal(raw, &n); err != nil {
			return CheckContext{}, false
		}
		return CheckContext{
			Name:       n.Name,
			Status:     normalizeRunStatus(n.Status),
			Conclusion: strings.ToLower(n.Conclusion),
			DetailsURL: n.DetailsURL,
		}, true
	case "StatusContext":
		var n struct {
			Context   string `json:"context"`
			State     string `json:"state"`
			TargetURL string `json:"targetUrl"`
		}
		if err := json.Unmarshal(raw, &n); err != nil {
			return CheckContext{}, false
		}
		status, conclusion := normalizeStatusState(n.State)
		return CheckContext{
			Name:            n.Context,
			Status:          status,
			Conclusion:      conclusion,
			DetailsURL:      n.TargetURL,
			IsStatusContext: true,
		}, true
	default:
		return CheckContext{}, false
	}
}

func normalizeRunStatus(s string) string {
	switch strings.ToUpper(s) {
	case "COMPLETED":
		return "completed"
	case "IN_PROGRESS":
		return "in_progress"
	default:
		return "queued"
	}
}

func normalizeStatusState(s string) (status, conclusion string) {
	switch strings.ToUpper(s) {
	case "SUCCESS":
		return "completed", "success"
	case "FAILURE":
		return "completed", "failure"
	case "ERROR":
		return "completed", "error"
	case "PENDING":
		return "in_progress", ""
	default:
		return "queued", ""
	}
}
