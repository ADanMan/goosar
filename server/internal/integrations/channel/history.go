package channel

type HistoryRole string

const (
	HistoryRoleUser HistoryRole = "user"

	HistoryRoleAssistant HistoryRole = "assistant"
)

type HistoryMessage struct {
	ID string `json:"id"`

	Author string `json:"author"`

	AuthorID string `json:"author_id,omitempty"`

	Role HistoryRole `json:"role"`

	Text string `json:"text"`

	TS string `json:"ts"`

	ThreadID string `json:"thread_id,omitempty"`

	ReplyCount int `json:"reply_count,omitempty"`

	LatestReply string `json:"latest_reply,omitempty"`
}

type HistoryPage struct {
	ChannelType string `json:"channel_type,omitempty"`

	ThreadID string `json:"thread_id,omitempty"`

	Messages []HistoryMessage `json:"messages"`

	NextCursor string `json:"next_cursor,omitempty"`
}

type HistoryOptions struct {
	Limit int

	Before string
}
