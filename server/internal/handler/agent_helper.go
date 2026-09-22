package handler

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

const (
	helperAgentSystemKey = "goosar_helper"

	helperAgentName = "Goosar Helper"

	helperAgentTemplate = "goosar_helper"

	helperAgentMaxConcurrentTasks = 6

	helperAgentAvatarURL = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAxMDI0IDEwMjQiPjxyZWN0IHdpZHRoPSIxMDI0IiBoZWlnaHQ9IjEwMjQiIHJ4PSIyMjQiIGZpbGw9IiNGM0VGRTYiLz48ZyBmaWxsPSIjMjIzMDNDIiBmaWxsLXJ1bGU9ImV2ZW5vZGQiIHRyYW5zZm9ybT0idHJhbnNsYXRlKDExMiAxMTIpIHNjYWxlKC43OCkiPjxwYXRoIGQ9Im01NzggMTE0IDEgOCA2LTFxOCAwIDcgMmwxIDFoNGwzIDMgMSAyIDEgNi0yIDExLTggMTBxLTYgNi00IDdsMiAzIDEgMy01IDctNyA1LTQgMi0zIDMtNyA0LTYgNS0yIDJjLTEgMS0yIDMtMSAxNGwtMTIgMTItMTYgMTUgNCAxIDQgMSAzIDEgOSAyIDUgMiAyIDEgNSAyIDMgMSA1IDIgNyA0IDQgMiA1IDMgMjEgMTYgNSA3IDIgMiAyIDV2MTBsLTQgMTAtMiA2LTIgNS0zIDYtMSA1LTEgMi0yIDMtMiA2LTEgNC0xIDMtOCAxOS0xMiAzMS0yIDQtMSAyLTMgNHEtMiAyLTIgOGExNjIgMTYyIDAgMCAxLTIgNDVsLTEgMy0xIDQtMSAyLTIgNS0yIDYtMSA0LTEgMi0yIDUtMSA0LTEgMi0xIDYtMiA2LTIgNi0xIDctMiA5YTI2OSAyNjkgMCAwIDAtNCA2MnExIDEgNi0ybDctMyA0IDIgMiAxNiAyIDI3IDIzLTFhMzM1IDMzNSAwIDAgMSA3MSA3bDYgMSA2IDIgMyAxIDcgMiA2IDIgNiAyIDQgMSAzIDEgNyAzIDcgMyAxIDEgMiAxIDEzIDYgMTMgNyAzIDEgMyAyIDYgNCA4IDYgMyAyIDYgNHE2IDQgNCA3bC05IDIwLTMgNC0zIDQtMTcgMjctMyA0LTMgNC0zIDQtOCA4YTU4MCA1ODAgMCAwIDEtNTIgNDVsLTggNS0zIDItNiA0LTQ3IDIxcS00IDMtOSAzbC00IDItNiAyLTcgMS03IDItMTEgMi0xNSAyLTE2IDJhMTYxIDE2MSAwIDAgMS00NS0xbC0xMS0xLTE3LTMtOC0xLTYtMi01LTJxLTUgMC02LTNsLTgtMjEtMi02LTktNDAtNCAxLTE0IDUtMzAgMnY1bDEgNiAyIDIyLTItMS0zLTItMi0xLTQtM3EtMy0yLTMtNWwtMS00LTItMTAtMS0xMC0xLTNxMS0yLTQtM2wtNS0xLTMtMS01LTQtMy0yLTctN2E2NSA2NSAwIDAgMS0xNS0zMCAxMTMgMTEzIDAgMCAxIDAtMzNsMy04IDQtNiA2LTQgNy0yIDUgMXExIDIgMi0xdi01bC0yLTgtMS02LTItMTAtMS0xMS0yLTEwLTQtMjktMy0xMnEtMS0zLTEgNWwtMSA2LTEgMTMtMiAxNC02IDE1LTUgNHEtNCAzLTYgMmwtNS01LTEtMTYtMS0xMy0yIDMtMiA2LTEgNC0xIDEtMiA1LTQgOC00IDYtMyA1LTUgNC00IDItMS0yLTMtMXEtMiAyLTMtMmE3MSA3MSAwIDAgMSAyLTI0bDEtNy0zIDMtMiAzLTQgNS00IDJxLTEtMi0xIDEtMSAyLTMgMmwtNS0ycS0zLTItMi0xMGwxLTggMS0zIDEtNSA3LTE3cS0yLTEtNSAyLTQgMy05IDN0LTUtNHYtOGwyLTUgMy01IDctMTJhMjU1IDI1NSAwIDAgMCAyMy01M2wyLTMgMi0yIDEtNCA1LTUgNS00IDMtMiAyLTEgMS0xIDQtMiAxMS0yYzcgMCA3LTEgOC00bC0xLTUtMS02LTMtMjVxMC04IDMtOGg2bDQgNiAxIDggMSA1IDIgMTIgMiAxNXEwIDUgMyA2bDggNiA3IDkgMiA3IDEgMiAxIDEyLTEgMTEtMSA2LTIgNi0yIDYtMyA3LTEgNiAzIDEzIDIgMTAgMSAxMCAyIDEwIDEgMTEgMiAxMSAyIDE0IDEgNiAyIDExIDEgMTAgMSAxMmgxMGwxMS0xIDctMSA2LTIgMy0xIDMtMSAyLTEgNy0yIDI0LTE1IDIwLTE5di0xM2wxLTIycTAtMTAgMy05bDIgMSAxIDEgNCAyIDQgMSAyLTUgMy04IDEtNSAyLTYgMS02IDItOCA0LTM0LTEtMTYtMS0zLTItNy0zLTlxLTEtMy05LTEwaC0xM2wtMjkgMS00LTEtOS0zcS01LTItNy01bC0xLTdxMS00IDQtN2w5LTcgMTItOHE0LTIgMTctMTd2LTEwcTAtMTAtMi0xMmwtMS03IDEtNSAyLTQgNi01IDQtM3YtM2wtMTAtNC0xMS01cS00LTItMy02bDEtNSAzLTEgMS0xIDEtMSA1LTMgNS0yIDMtMSA2LTMgNC0xIDItMSAzLTMgMi0xMiAxLTEwIDEtNiA2LTM1IDEtNiAxLTggMi0xMCAxLTUgMS00IDEtNnExLTUgNS05bDUtNCAyLTEgOS0yIDEwLTFoNGwxLTIyYTEyMCAxMjAgMCAwIDEgNS0zNWwzLTcgNC00cTMgMCA2LTZsMi02IDEtMSAxLTEgNC03IDUtOCA3LTkgMTctMTRoM2wyLTJ6bS0yMCAxNS0xMCAxMC0yIDMtMiAyLTIgNC02IDEwLTMgNi0xIDItMiA1LTIgNi0xIDUtMyAxLTEtMi0xLTItMiAyLTMgMTEtMSAzOSAyLTYgMS01IDEtNSA0LTEwIDEtNSAyLTMgMi0zIDYtMTEgMTgtMjMgNi01LTEgMi00IDQtNCA1LTIgMy0yIDQtNCA1LTIgNS0yIDMtMSAyLTQgOS00IDExLTIgMy0xIDItMSA0LTMgOC0xIDctMSAzcTAgNCAxIDJsMTctMTcgNS01IDUtNy0xLTItMy0yIDMtNXEyLTUgNy03bDUtNSA5LTYgNC0zIDUtMyA0LTIgMi00aC00bC01LTFxLTMtMi0xIDFoLTFxLTItMy0xLTRsMi0xIDUtNCAxOC0yMCAxLTYtMy00LTgtMS04IDEtMiAxLTMgMi04IDUtMS0yIDItNSAzLTUgMi0zIDEtMS0yLTEtNCAyem0tNTcgMTI0LTEgMi0xIDcgMiAxMCA1IDUgNiAyIDYtMyA0LTcgMi00IDEtNi0xLTUtMS0zLTMtNHEtMi0zLTctMy00IDAtOCA0LTQgMy00IDVtLTY2IDE5IDEgMS0yIDEtMyAxLTUgMS01IDItMiAxLTUgMi03IDMtNSAxLTIgMS0zIDItMTAgNC04IDMtNiAzLTUgNC00IDItNSAyLTMgMi00IDMtMTAgNi03IDQtMSAxLTUgNC01IDMtMyAzLTkgOGE0NjUgNDY1IDAgMCAwLTUzIDUzbC0xLTJ2M2wtMSA0LTIgMy0xIDItNSA0LTMgNS00IDctNCA3LTEgMS0zIDZhNjU1IDY1NSAwIDAgMC0yOCA2N2wtMiA2LTEgNi0yIDYtMSA0LTUgMjNhNDgyIDQ4MiAwIDAgMCAxMiAxMzBsMSAyIDIgOCAyIDYgMiA0IDIgNiAxIDQgMiA0IDIgMyAxIDMgMSAyIDIgMyAxIDMgMiAzIDIgNSAyIDUgMiAzIDIgNCAxNyAyNCAyIDQgMyAzIDIgMyAzIDQgMTIgMTVhMjQyIDI0MiAwIDAgMCA0MCAzNmwzIDMgMyAyIDMgMyA2IDMgNCA0IDIgMiA0IDEgNSA0IDggNSA1MiAyNCA3IDIgMTAgNHYtMWwtNC0yLTktMy0yMS05LTQ0LTI0LTMtMi02LTQtMjgtMjItMjQtMjUtMTctMjAtMTgtMjYtMjEtMzktMi02LTEtNC0xLTMtMi00di00bC0xLTEtMS0yaDJsMSA1IDEgMiAyIDNxMCA0IDMgOGwyMyA0NCAzIDMgMyA1IDMgNCAzIDUgNCA1IDEgMSA0IDZhNzQ3IDc0NyAwIDAgMCA2MSA1NmwyMyAxNSA2NyAzMCA1IDIgNSAxIDYgMiA2IDEgNCAxIDMgMSA4IDEgOSAyIDE2IDJhMzkyIDM5MiAwIDAgMCA5MC01bDMtMSA0LTEgNi0xIDYtMiA1LTEgNC0yIDUtMSA1LTIgMTEtNCAxLTEgNC0xIDIwLTEwIDMtMiA1LTIgNS0yIDMtMiA0LTNhMjY4IDI2OCAwIDAgMCA1NC0zOWwyMy0yNSA3LTggOC0xMCA5LTEzIDktMTQgMy01IDQtNyAxNS0zMSAxLTMgMy02IDItNyAxLTMgMi03IDItNiAxIDItMSAzLTEgNS0yIDUtMSA1LTIgNS0xIDItMSA1LTM2IDY3LTIyIDMwYTI3MSAyNzEgMCAwIDEtNTMgNDZsLTIgMy05IDYtNyA0LTEgMS0yIDEtNyA3IDMtMiAyLTIgNC0xIDI5LTIwIDUtMyAxMS0xMCAzOS00MCAxOS0yNSA0LTcgMy00IDQtNyAyLTMgMi0zIDMtNyA4LTE2IDItMyAyLTUgMi01IDEtMiAzLTYgMy05IDEtNCAyLTYgMS01IDItNSAxLTYgMS00YTQxMyA0MTMgMCAwIDAgMy0xMjJsLTItOS0xLTUtMS0zLTEtNi0yLTYtMS01LTItNi00LTExYTI2NyAyNjcgMCAwIDAtMjYtNTVsLTI2LTM3LTQtNS0zMy0zNS0yMC0xOC0xLTJoLTR2LTJxMi0xLTEtMS0yIDItMi0xbC0yLTFxLTIgMC0xLTJsLTEtMWgtM3EtMiAwLTEtMmgtMmwtMS0xcTEtMS0xLTFsLTctNC01LTMtNi0zLTE1LTlxLTEwLTQtMTAtNmgzbDE1IDYgNDUgMjggMjEgMTZxNCA1IDYgNWwxMiAxMiAxNiAxOCAzMCAzOSAyIDUgMyA1IDEwIDE5IDQgNyA0IDExIDIgMyAxIDIgMSA0IDMgNyAxIDQgMiA0IDEgNSAxIDMgMSAzIDIgNiAxIDUgMiA3IDEgOCAyIDExIDMgMjQgMSAyOGEyMDkgMjA5IDAgMCAxLTQgNTFsLTExIDQ2LTIgNi0yIDUtMSAzLTQgMTAtNCAxMC0xIDMtMiAzLTEgMi0xIDMtMiAzLTQgNy0xMCAxNy0zIDUtNiA5LTEgMS0yIDMtMyA0LTcgOWE2MzIgNjMyIDAgMCAxLTUwIDUxbC01IDMtMiAxLTUgNS01IDMtMSAxLTMgMi0zNyAyMi00MyAxOC02IDItNyAzLTggMi03IDEtNyAyLTkgMi0zNSA1LTI0IDFhMTIxIDEyMSAwIDAgMS0zMi0ybC00OC05LTMtMS0zLTEtNS0yLTUtMS01LTEtNi0yLTktNGEyNDcgMjQ3IDAgMCAxLTU1LTI4bC00MC0yOWE0OTIgNDkyIDAgMCAxLTU2LTYybC0yLTQtMi0yLTQtNy01LTgtMy02LTMtNS0xLTEtNC05LTEtMS0zLTctMy05LTItMy0yLTQtMi02LTEtMy0xLTItMS01LTQtMTAtMi03LTEtNi0yLTUtMS04LTItOC0yLTEyYTQ3MyA0NzMgMCAwIDEgMy0xMDdsMi04IDItNyAxLTYgMS0zIDEtMyAxLTQgMi04IDItNCAxLTMgMS0yIDItNiAyLTQgMS0yIDItN2EzMzEgMzMxIDAgMCAxIDIyMC0xODRtODIgMTIwYTQ4MyA0ODMgMCAwIDEtNjUgM2wzIDcgMiAxMS0xIDYtMyAyLTQgMi0yIDItMiAzLTIgM3EwIDMgMSAybDQtNCAzLTQgMi0ycTItMiAxMS0ydDEwIDJsNCAycTMgMCA1IDZsMyA2IDIgNiA0IDEwIDIgNCA3IDEwIDEgMSAxIDUtMiA4LTMgMy01IDMtOCA0LTcgMi00IDEtMyAxLTMgMSAyIDIgMTAgMSAxNC0yIDgtNCAzLTIgNy03cTctNiAxMC0xMmExMTYgMTE2IDAgMCAwIDE1LTMybDEtMiAyLTQgMS01IDEtNCAxLTMgMi03IDEtNyAyLTdjMC02IDAtNi0zLThsLTUtNGgtOHptLTc5IDE3cS0zIDUtMSA4bDQgNnEyIDIgNi0xIDMgMCA0LTRsMS03cTAtNi0yLTdsLTUtMi0zIDJ6bTExNyAyLTMgMTMtMSA3LTIgNy0xIDQtMiA3LTIgNC0xIDItMSAyLTMgNy0xMCAxNi0zIDMtMyAzLTIgMy0zIDItMiAyLTkgNi0xMiA1LTQgMS05IDEtOSAxIDIgNSAyIDExYTE1NSAxNTUgMCAwIDEtMiA1MmwtMSA3LTIgNy0xIDMtNSAxOS0yIDUtMSAyIDIgMSAxIDIgMSAzIDIgOCAzIDE4cTIgMSA3LTExbDYtMTMgMi0yIDUtMiA2LTIgMzYtMTggMS0ydi0xOGE0MDMgNDAzIDAgMCAxIDktNzJsMS01IDEtNCAyLTQgMS01IDItNSAxLTQgMS0zIDEtMyAyLTQgMS00IDItNSAxLTZxMi0xIDItMjQgMC0yNC0yLTI0bC0xLTQtMS0yaC0xem0tMTAxIDIxYTIxMCAyMTAgMCAwIDEtNDggNDNsLTYgNXEtMiA0IDIgNiAyIDMgMyAybDIgMSAyIDEgMyAxaDE4bDItMSAzLTEgMy0yIDMtMSAzLTIgNS0yIDQtMiAyLTEgMi0xIDUtMiA3LTFxMi0xIDggMWw3IDItMSAycTAgMy00IDBsLTctMS00IDEtNCAxLTEwIDQtNiA0IDEwIDEgMTAtMSA3LTEgNy0yIDItMSA1LTJxNC0xIDYtNCAzLTIgMy03bC0yLTZxLTMtMi01LTZsLTgtMTgtNy0xMy00LTItMy0xLTQtMXEtNCAwLTcgM3ptLTE1OCA2OS00IDEtMyAxLTYgMy01IDMtNCAzLTYgOS0xIDItMiA0LTQgOS0xNCAzNS0zIDRxLTMgMi0xIDJ2MWwtMyAzLTggMTggMSA0IDctNCA3LTYgNi03IDctNiAxIDMtNSAxMy02IDE0LTIgMy0yIDMtMSA1LTIgNi0yIDlxLTEgNiAyIDVsNy00IDUtNyAyLTQgMy00IDQtNyAyLTYgMS0xIDMtN3EzLTcgNS03IDMtMSAyIDVsLTIgNS0xIDQtMSAyLTEgNS0yIDUtMiA0LTEgNi0yIDYtMiA5LTEgNS0xIDUgMSA1IDIgMiA0LTMgNC01IDItMyAxLTIgMS0xIDMtOCAxOS00MXY0OXExIDQgMyA0bDUtNCA0LTggMS00IDItMTQgMi02OS0xMC01My0zLTEtMiAxem0yNiA5IDEgNSAxIDUgMiAxMSA1IDMwIDEtMiAxLTMgMi02IDItMTQtMS0xMS0zLTgtNi04LTQtMy0yIDJ6bTIxMCAxNjgtNCA1cS0yIDQtNiA0bC05IDJhMzE2IDMxNiAwIDAgMS03OC0zbC0xIDEtMSAzcS0xIDQgMyAzbDMgMSA3IDEgNiAyIDkgMWEzOTkgMzk5IDAgMCAwIDY4LTJsMyAzIDcgMnE0IDAgNy00YzQtMyA0LTMgNC05cTAtNi00LThsLTYtM2gtNHptLTIxNSAyNy0yIDUtMSAzLTEgMTMgMSAxNyAyIDUgMSAzIDIgNiA2IDkgMTIgMTEgNiA0IDggMnE1IDEgOC0zbDQtNiAyLTE0di0xMGwtMTQtMTItNS0zLTQtMy0xNS03LTItMiAxLTJoM2w0IDEgOCAzYTEzNiAxMzYgMCAwIDEgMjcgMTlxNSA0IDUgMmwyLTQtMS0zLTEtMy0yLTYtMy03LTctOS0xMS0xMS0xMC01LTEwLTJxLTctMS05IDN6bTE5OCAzMC0xMCAyLTEyIDEtMTggMS00My02LTctMWgtM2wtMSA0cS0xIDMgNSA0bDcgMiAyIDEgNiAxIDggMiAyNSAxYTEyNCAxMjQgMCAwIDAgMzUtMmM3LTEgNy0xIDExIDJxNCAzIDcgM2w2LTEgNS0zIDEtOHExLTUtMS04bC01LTMtNi0xcS00IDAtOSA0em0tOSA0Ni01IDRhMzgyIDM4MiAwIDAgMS04MS0ybDEgNCA0IDIgNCAxIDcgMiA2IDEgNiAxaDQ5bDYtMXExLTIgNCAxbDQgMyAxIDEgNCAxIDctMSA0LTQgMS04LTEtNy02LTMtNS0yYTE2IDE2IDAgMCAwLTEwIDdtLTE2LTQ4NWExOCAxOCAwIDAgMC0yIDE1cTEgNCA1IDcgMyAzIDEwIDMgNiAwIDktM2wzLTUgMS0xIDEtNmMwLTYgMC03LTQtMTFxLTMtNS05LTUtNSAwLTkgMnptLTUwLTE0cTMgMCAyIDFsLTUgMi02IDEtNCAyLTUgMS01IDEtNCAyLTIgMS00IDEtMjYgMTItMzggMjItMjEgMTUtNCAzLTUgNC04IDctMjIgMjMtMTAgMTItMTggMjMtNiAxMC01IDctMTUgMjktMiA2LTEgMS0yIDQtMSA0LTEgMi0yIDUtMiA1LTEgNS0yIDUtMSAzdi0zbDEtNiAyLTUgMy05IDEtMyAyLTQgMi03IDEtNCAxLTEgMi01IDQtOSA0LTggNS0xMCA0LTcgOC0xMiA1LTggMjItMjYgOS0xMCA0LTQgMTQtMTMgNS0zIDE2LTEzIDQtMyA1LTMgNC0zIDEtMSA4LTQgMjMtMTIgMTAtNSA2LTIgMi0xIDEtMSA1LTIgNS0yIDUtMSA0LTIgNi0yIDYtMSA2LTIgNi0xem0yMDAgMTcgMyAyIDEzIDYgMiAxIDcgMyA3IDQgNiA0IDUgNCAyIDEgNiAzIDUgMyA0IDMgNSA0IDEgMSA3IDUgMjEgMjAgNDMgNTIgMiA1IDMgMyA0IDggNiAxNCA2IDEyYTI2OSAyNjkgMCAwIDEgMTkgNjNsMiA4IDEgOC0xIDUtMS00LTEtOC01LTIxLTEtMi0xLTItMi04LTYtMTgtMi01LTMtNWEzMTQgMzE0IDAgMCAwLTE1Ni0xNjVsLTMtMXYtM3pNNTE0IDQwM2w0IDEtMyAyLTMgMS0zIDEtMyAyLTMgNXEtMyAzIDAgN2wzIDZxMiAyIDggMnQ4LTNxNC0yIDMtOCAxLTUtMi04bC00LTNxLTIgMC0xLTJoM3E0IDAgNiA1IDMgNCAyIDEwbC0yIDYtMSAyLTUgNC03IDItMy0xLTUtMnEtMyAwLTUtNGwtMy03cS0xLTQgMi05IDEtNiA2LTd6bS03MCA1IDIgMnY0bC0yIDItNS00IDEtM3ptMTkgMzkgMiAzcTAgMy0yIDJsLTMgMWgtMWwxLTR6bTUzLTMzIDQgMSAxIDQtMyAzcS0zIDEtNC0xLTMtMi0xLTR6Ii8+PC9nPjwvc3ZnPg=="

	helperHermesProvider = "runtime-j"

	helperOwnerLabelMaxRunes = 40

	helperNameNumberedAttempts = 32
)

type helperAnnounceMode int

const (
	helperAnnounceFull helperAnnounceMode = iota

	helperAnnounceQuiet
)

func helperLockKey(workspaceID pgtype.UUID) string {
	return "workspace-helper:" + uuidToString(workspaceID)
}

func ownsLiveAgent(agents []db.Agent, userID string) bool {
	for _, a := range agents {
		if a.ArchivedAt.Valid {
			continue
		}
		if a.OwnerID.Valid && uuidToString(a.OwnerID) == userID {
			return true
		}
	}
	return false
}

func (h *Handler) helperProvisionable(ctx context.Context, q *db.Queries, workspaceID, memberUserID pgtype.UUID) bool {
	allowed := h.cfg.AllowedProviders.AllowedList()
	state, err := q.MemberHelperProvisionable(ctx, db.MemberHelperProvisionableParams{
		WorkspaceID:       workspaceID,
		MemberUserID:      memberUserID,
		HelperSystemKey:   pgtype.Text{String: helperAgentSystemKey, Valid: true},
		RestrictProviders: allowed != nil,
		AllowedProviders:  allowed,
	})
	if err != nil {
		slog.Warn("member helper: provisionable probe failed",
			"workspace_id", uuidToString(workspaceID),
			"user_id", uuidToString(memberUserID),
			"error", err)
		return false
	}
	return state.IsMember && state.SlotFree && state.HasUsableRuntime
}

type helperProvisionResult struct {
	Agent        db.Agent
	Created      bool
	IsFirstAgent bool
}

func (h *Handler) ensureMemberHelper(ctx context.Context, q *db.Queries, workspaceID, memberUserID pgtype.UUID, acceptLanguage string) (helperProvisionResult, error) {
	var none helperProvisionResult
	if err := q.LockWorkspaceHelperKey(ctx, helperLockKey(workspaceID)); err != nil {
		return none, err
	}

	if !h.helperProvisionable(ctx, q, workspaceID, memberUserID) {
		return none, nil
	}

	member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      memberUserID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {

			return none, nil
		}
		return none, err
	}

	runtime, ok, err := h.pickHelperRuntime(ctx, q, workspaceID, member)
	if err != nil {
		return none, err
	}
	if !ok {
		return none, nil
	}

	lang := helperDefaultContentLang
	owner, err := q.GetUser(ctx, memberUserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return none, err
	}
	if err == nil {
		lang = helperContentLang(owner.Language.String, acceptLanguage)
	}

	baseName := helperAgentName
	instructions := helperInstructionsByLang[lang]
	if defaults, err := q.GetWorkspaceHelperDefault(ctx, workspaceID); err == nil {
		if v := workspaceTemplateText(parseWorkspaceTemplateLangMap(defaults.HelperName), lang); v != "" {
			baseName = v
		}
		if extra := workspaceTemplateText(parseWorkspaceTemplateLangMap(defaults.HelperExtraInstructions), lang); extra != "" {
			instructions = instructions + "\n\n" + extra
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return none, err
	}

	takenNames, err := q.ListWorkspaceAgentNames(ctx, workspaceID)
	if err != nil {
		return none, err
	}
	name, ok := helperAgentNameFor(owner, takenNames, baseName)
	if !ok {
		slog.Warn("member helper: no free name available",
			"workspace_id", uuidToString(workspaceID), "user_id", uuidToString(memberUserID))
		return none, nil
	}

	visibility := "private"
	permissionMode := permissionModePrivate
	if runtime.Visibility == "public" {
		visibility = "workspace"
		permissionMode = permissionModePublicTo
	}

	created, err := q.CreateHelperAgent(ctx, db.CreateHelperAgentParams{
		WorkspaceID:        workspaceID,
		Name:               name,
		Description:        helperDescriptionByLang[lang],
		AvatarUrl:          pgtype.Text{String: helperAgentAvatarURL, Valid: true},
		RuntimeMode:        runtime.RuntimeMode,
		RuntimeID:          runtime.ID,
		Visibility:         visibility,
		PermissionMode:     permissionMode,
		MaxConcurrentTasks: helperAgentMaxConcurrentTasks,
		OwnerID:            memberUserID,
		Instructions:       instructions,
	})
	if err != nil {
		return none, err
	}

	if permissionMode == permissionModePublicTo {
		if err := replaceInvocationTargetsWithQueries(ctx, q, created.ID, memberUserID, []targetSpec{
			{targetType: invocationTargetWorkspace, targetID: workspaceID},
		}); err != nil {
			return none, err
		}
	}

	return helperProvisionResult{Agent: created, Created: true, IsFirstAgent: len(takenNames) == 0}, nil
}

func (h *Handler) provisionMemberHelper(ctx context.Context, workspaceID, memberUserID pgtype.UUID, acceptLanguage string, announce helperAnnounceMode) bool {
	wsID := uuidToString(workspaceID)

	if !h.helperProvisionable(ctx, h.Queries, workspaceID, memberUserID) {
		return false
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Warn("member helper: begin failed", "workspace_id", wsID, "error", err)
		return false
	}
	defer tx.Rollback(ctx)

	result, err := h.ensureMemberHelper(ctx, h.Queries.WithTx(tx), workspaceID, memberUserID, acceptLanguage)
	if err != nil {
		slog.Warn("member helper: provisioning failed", "workspace_id", wsID, "error", err)
		return false
	}
	if !result.Created {
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("member helper: commit failed", "workspace_id", wsID, "error", err)
		return false
	}

	slog.Info("member helper provisioned",
		"workspace_id", wsID,
		"user_id", uuidToString(memberUserID),
		"agent_id", uuidToString(result.Agent.ID))
	h.announceHelperCreated(ctx, wsID, result.Agent, result.IsFirstAgent, announce)
	return true
}

func (h *Handler) announceHelperCreated(ctx context.Context, workspaceID string, helper db.Agent, isFirstAgent bool, announce helperAnnounceMode) {

	actorID := uuidToString(helper.OwnerID)
	actorType := "member"
	if actorID == "" {
		actorType = "system"
	}

	provider := ""
	runtimeOnline := false
	if runtime, err := h.Queries.GetAgentRuntime(ctx, helper.RuntimeID); err == nil {
		provider = runtime.Provider
		runtimeOnline = runtime.Status == "online"
	}

	if runtimeOnline {
		h.TaskService.ReconcileAgentStatus(ctx, helper.ID)
		if refreshed, err := h.Queries.GetAgent(ctx, helper.ID); err == nil {
			helper = refreshed
		}
	}

	resp := broadcastAgentResponse(h.agentToResponse(helper))
	h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": resp})

	if announce == helperAnnounceFull {
		h.sendAgentWelcomeChat(ctx, helper, actorID, workspaceID)
	}

	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.AgentCreated(
		actorID, workspaceID, uuidToString(helper.ID),
		provider, helper.RuntimeMode, helperAgentTemplate, isFirstAgent,
	))
}

func (h *Handler) pickHelperRuntime(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, member db.Member) (db.AgentRuntime, bool, error) {
	runtimes, err := q.ListAgentRuntimes(ctx, workspaceID)
	if err != nil {
		return db.AgentRuntime{}, false, err
	}

	unprivileged := member
	unprivileged.Role = "member"

	usable := func(rt db.AgentRuntime) bool {
		return rt.Status == "online" &&
			h.cfg.AllowedProviders.Allows(rt.Provider) &&
			canUseRuntimeForAgent(unprivileged, rt)
	}
	isOwn := func(rt db.AgentRuntime) bool {
		return rt.OwnerID.Valid && uuidToString(rt.OwnerID) == uuidToString(member.UserID)
	}

	ranks := []func(db.AgentRuntime) bool{
		func(rt db.AgentRuntime) bool { return isOwn(rt) && rt.Provider == helperHermesProvider },
		func(rt db.AgentRuntime) bool { return isOwn(rt) },
	}
	for _, matches := range ranks {
		for _, rt := range runtimes {
			if matches(rt) && usable(rt) {
				return rt, true, nil
			}
		}
	}
	return db.AgentRuntime{}, false, nil
}

func helperOwnerLabel(owner db.User) string {
	label := strings.Join(strings.Fields(owner.Name), " ")
	if label == "" {
		email := strings.TrimSpace(owner.Email)
		if at := strings.IndexByte(email, '@'); at > 0 {
			email = email[:at]
		}
		label = strings.Join(strings.Fields(email), " ")
	}
	if runes := []rune(label); len(runes) > helperOwnerLabelMaxRunes {
		label = strings.TrimSpace(string(runes[:helperOwnerLabelMaxRunes]))
	}
	return label
}

func helperNameCandidates(baseName, label, ownerID string) []string {
	short := strings.ReplaceAll(ownerID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}

	candidates := []string{baseName}
	if label != "" {
		candidates = append(candidates,
			baseName+" ("+label+")",
			baseName+" ("+label+" "+short+")",
		)
	}
	candidates = append(candidates, baseName+" ("+short+")")
	for n := 2; n <= helperNameNumberedAttempts; n++ {
		candidates = append(candidates, baseName+" ("+short+" "+strconv.Itoa(n)+")")
	}
	return candidates
}

func helperAgentNameFor(owner db.User, takenNames []string, baseName string) (string, bool) {
	taken := make(map[string]struct{}, len(takenNames))
	for _, name := range takenNames {
		taken[name] = struct{}{}
	}
	for _, candidate := range helperNameCandidates(baseName, helperOwnerLabel(owner), uuidToString(owner.ID)) {
		if _, clash := taken[candidate]; !clash {
			return candidate, true
		}
	}
	return "", false
}
