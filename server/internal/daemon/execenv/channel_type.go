package execenv

const (
	ChannelTypeSlack  = "slack"
	ChannelTypeFeishu = "feishu"
)

var channelDisplayNames = map[string]string{
	ChannelTypeSlack:  "Slack",
	ChannelTypeFeishu: "Feishu",
}

func ChannelDisplayName(channelType string) string {
	if name, known := channelDisplayNames[channelType]; known {
		return name
	}
	return channelType
}
