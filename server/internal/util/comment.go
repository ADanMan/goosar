package util

func ExposeCommentSourceTaskID(authorType, commentType string) bool {
	return authorType == "agent" && commentType == "system"
}
