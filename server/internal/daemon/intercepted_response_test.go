package daemon

import "testing"

func TestDescribeInterceptedResponse(t *testing.T) {
	page := `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN">
<html><head><title>McAfee Web Gateway - Notification</title></head>
<body>The transferred file contained a virus and was therefore blocked.
<b>Virus Name: </b>BehavesLike.VBS.Dropper.mv<br /></body></html>`

	got, ok := describeInterceptedResponse(page)
	if !ok {
		t.Fatal("an HTML gateway page must be recognised, not passed through")
	}
	if len(got) > 200 {
		t.Fatalf("the description must be a sentence, not the page: %d chars", len(got))
	}
	if !contains(got, "BehavesLike.VBS.Dropper.mv") {
		t.Fatalf("the verdict names WHY it was blocked and must survive: %q", got)
	}
	if contains(got, "<html") || contains(got, "DOCTYPE") {
		t.Fatalf("no markup may reach the message: %q", got)
	}
}

func TestDescribeInterceptedResponseWithoutVerdict(t *testing.T) {
	got, ok := describeInterceptedResponse("<html><body>Access denied by policy</body></html>")
	if !ok {
		t.Fatal("an HTML page must be recognised even with no virus name")
	}
	if contains(got, "<") {
		t.Fatalf("no markup may reach the message: %q", got)
	}
}

func TestDescribeInterceptedResponseIgnoresApiErrors(t *testing.T) {
	if _, ok := describeInterceptedResponse(`{"error":"task not found"}`); ok {
		t.Fatal("a JSON API error must not be mistaken for an interception")
	}
	if _, ok := describeInterceptedResponse("task is not preparing"); ok {
		t.Fatal("a plain-text API error must not be mistaken for an interception")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestRequestErrorHidesGatewayPage(t *testing.T) {
	page := `<!DOCTYPE html><html><head><title>McAfee Web Gateway - Notification</title></head>
<body><b>Virus Name: </b>BehavesLike.VBS.Dropper.mv<br /></body></html>`
	err := &requestError{
		Method:     "POST",
		Path:       "/api/daemon/runtimes/x/tasks/y/skill-bundles/resolve",
		StatusCode: 403,
		Body:       page,
	}

	message := err.Error()
	if contains(message, "<html") || contains(message, "DOCTYPE") {
		t.Fatalf("markup leaked into the error text: %q", message)
	}
	if !contains(message, "BehavesLike.VBS.Dropper.mv") {
		t.Fatalf("the actionable verdict must survive: %q", message)
	}
	if err.Body != page {
		t.Fatal("Body must stay verbatim for programmatic matching")
	}
}

func TestRequestErrorKeepsApiErrorVerbatim(t *testing.T) {
	err := &requestError{Method: "POST", Path: "/api/x", StatusCode: 409, Body: "task is not preparing"}
	if !contains(err.Error(), "task is not preparing") {
		t.Fatalf("api error text was altered: %q", err.Error())
	}
}
