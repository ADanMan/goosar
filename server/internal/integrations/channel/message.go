package channel

import "encoding/json"

type ChatType string

const (
	ChatTypeP2P ChatType = "p2p"

	ChatTypeGroup ChatType = "group"
)

type MsgType string

const (
	MsgTypeText MsgType = "text"

	MsgTypeImage MsgType = "image"

	MsgTypeFile MsgType = "file"

	MsgTypeAudio MsgType = "audio"

	MsgTypeVideo MsgType = "video"

	MsgTypeUnknown MsgType = "unknown"
)

type Source struct {
	ChannelType Type

	ChatID string

	ChatType ChatType

	SenderID string

	SenderStableID string

	ThreadID string
}

type MediaRef struct {
	Type MsgType

	StorageKey string

	StorageURL string

	Filename string

	MimeType string

	SizeBytes int64
}

type ReplyCtx struct {
	MessageID string

	RootID string
}

type InboundMessage struct {
	EventID   string
	MessageID string

	Source Source

	Type MsgType

	Text string

	MediaRefs []MediaRef

	ReplyTo *ReplyCtx

	AddressedToBot bool

	ForceFresh bool

	Raw json.RawMessage
}

type OutboundMessage struct {
	ChatID string

	Text string

	ThreadID string

	ReplyTo string
}

type SendResult struct {
	MessageID string
}
