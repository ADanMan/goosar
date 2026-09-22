package channel

import "strings"

type Capability uint64

const (
	CapText Capability = 1 << iota

	CapRichCard

	CapThreadReply

	CapQuoteReply

	CapAttachment

	CapVoice

	CapTypingIndicator

	CapMessageEdit
)

var capabilityNames = []struct {
	bit  Capability
	name string
}{
	{CapText, "text"},
	{CapRichCard, "rich_card"},
	{CapThreadReply, "thread_reply"},
	{CapQuoteReply, "quote_reply"},
	{CapAttachment, "attachment"},
	{CapVoice, "voice"},
	{CapTypingIndicator, "typing_indicator"},
	{CapMessageEdit, "message_edit"},
}

func (c Capability) Has(want Capability) bool {
	return c&want == want
}

func (c Capability) String() string {
	if c == 0 {
		return "none"
	}
	var (
		parts     []string
		remaining = c
	)
	for _, cn := range capabilityNames {
		if remaining&cn.bit == cn.bit {
			parts = append(parts, cn.name)
			remaining &^= cn.bit
		}
	}
	if remaining != 0 {
		parts = append(parts, "0x"+strings.TrimLeft(hex(uint64(remaining)), "0"))
	}
	return strings.Join(parts, "|")
}

func hex(v uint64) string {
	const digits = "0123456789abcdef"
	var buf [16]byte
	for i := 15; i >= 0; i-- {
		buf[i] = digits[v&0xf]
		v >>= 4
	}
	return string(buf[:])
}
