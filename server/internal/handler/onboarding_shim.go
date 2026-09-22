// Устаревшие ручки онбординга, оставленные ради старых сборок desktop-клиента.
// Актуальный клиент создаёт Helper-агента и стартовые issue через общий
// фронтенд-хук и обычные CreateAgent/CreateIssue; этот файл — минимальная
// копия прежней реализации без сервисного слоя, нужная только пока не
// обновились все активные инсталляции. Поведение менять нельзя — контракт
// «то, чего ждёт старый клиент»; когда телеметрия подтвердит переход всех
// на новую версию, файл вместе с роутами и тестами можно удалить.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/issueguard"
	"github.com/adanman/goosar/server/internal/logger"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

const runtimeBootstrapBodyLimit = 8 * 1024

const maxStarterPromptLen = 2 * 1024

const (
	onboardingAssistantName = "Goosar Helper"
	onboardingIssueTitle    = "Start here: learn Goosar with Goosar Helper"
	onboardingAgentTemplate = "goosar_helper"

	noRuntimeIssueTitle = "Connect a runtime to start using agents"
)

const onboardingAssistantDescription = "Built-in workspace assistant. Answers Goosar questions and runs CLI operations."

const onboardingAssistantAvatarURL = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAxMDI0IDEwMjQiPjxyZWN0IHdpZHRoPSIxMDI0IiBoZWlnaHQ9IjEwMjQiIHJ4PSIyMjQiIGZpbGw9IiNGM0VGRTYiLz48ZyBmaWxsPSIjMjIzMDNDIiBmaWxsLXJ1bGU9ImV2ZW5vZGQiIHRyYW5zZm9ybT0idHJhbnNsYXRlKDExMiAxMTIpIHNjYWxlKC43OCkiPjxwYXRoIGQ9Im01NzggMTE0IDEgOCA2LTFxOCAwIDcgMmwxIDFoNGwzIDMgMSAyIDEgNi0yIDExLTggMTBxLTYgNi00IDdsMiAzIDEgMy01IDctNyA1LTQgMi0zIDMtNyA0LTYgNS0yIDJjLTEgMS0yIDMtMSAxNGwtMTIgMTItMTYgMTUgNCAxIDQgMSAzIDEgOSAyIDUgMiAyIDEgNSAyIDMgMSA1IDIgNyA0IDQgMiA1IDMgMjEgMTYgNSA3IDIgMiAyIDV2MTBsLTQgMTAtMiA2LTIgNS0zIDYtMSA1LTEgMi0yIDMtMiA2LTEgNC0xIDMtOCAxOS0xMiAzMS0yIDQtMSAyLTMgNHEtMiAyLTIgOGExNjIgMTYyIDAgMCAxLTIgNDVsLTEgMy0xIDQtMSAyLTIgNS0yIDYtMSA0LTEgMi0yIDUtMSA0LTEgMi0xIDYtMiA2LTIgNi0xIDctMiA5YTI2OSAyNjkgMCAwIDAtNCA2MnExIDEgNi0ybDctMyA0IDIgMiAxNiAyIDI3IDIzLTFhMzM1IDMzNSAwIDAgMSA3MSA3bDYgMSA2IDIgMyAxIDcgMiA2IDIgNiAyIDQgMSAzIDEgNyAzIDcgMyAxIDEgMiAxIDEzIDYgMTMgNyAzIDEgMyAyIDYgNCA4IDYgMyAyIDYgNHE2IDQgNCA3bC05IDIwLTMgNC0zIDQtMTcgMjctMyA0LTMgNC0zIDQtOCA4YTU4MCA1ODAgMCAwIDEtNTIgNDVsLTggNS0zIDItNiA0LTQ3IDIxcS00IDMtOSAzbC00IDItNiAyLTcgMS03IDItMTEgMi0xNSAyLTE2IDJhMTYxIDE2MSAwIDAgMS00NS0xbC0xMS0xLTE3LTMtOC0xLTYtMi01LTJxLTUgMC02LTNsLTgtMjEtMi02LTktNDAtNCAxLTE0IDUtMzAgMnY1bDEgNiAyIDIyLTItMS0zLTItMi0xLTQtM3EtMy0yLTMtNWwtMS00LTItMTAtMS0xMC0xLTNxMS0yLTQtM2wtNS0xLTMtMS01LTQtMy0yLTctN2E2NSA2NSAwIDAgMS0xNS0zMCAxMTMgMTEzIDAgMCAxIDAtMzNsMy04IDQtNiA2LTQgNy0yIDUgMXExIDIgMi0xdi01bC0yLTgtMS02LTItMTAtMS0xMS0yLTEwLTQtMjktMy0xMnEtMS0zLTEgNWwtMSA2LTEgMTMtMiAxNC02IDE1LTUgNHEtNCAzLTYgMmwtNS01LTEtMTYtMS0xMy0yIDMtMiA2LTEgNC0xIDEtMiA1LTQgOC00IDYtMyA1LTUgNC00IDItMS0yLTMtMXEtMiAyLTMtMmE3MSA3MSAwIDAgMSAyLTI0bDEtNy0zIDMtMiAzLTQgNS00IDJxLTEtMi0xIDEtMSAyLTMgMmwtNS0ycS0zLTItMi0xMGwxLTggMS0zIDEtNSA3LTE3cS0yLTEtNSAyLTQgMy05IDN0LTUtNHYtOGwyLTUgMy01IDctMTJhMjU1IDI1NSAwIDAgMCAyMy01M2wyLTMgMi0yIDEtNCA1LTUgNS00IDMtMiAyLTEgMS0xIDQtMiAxMS0yYzcgMCA3LTEgOC00bC0xLTUtMS02LTMtMjVxMC04IDMtOGg2bDQgNiAxIDggMSA1IDIgMTIgMiAxNXEwIDUgMyA2bDggNiA3IDkgMiA3IDEgMiAxIDEyLTEgMTEtMSA2LTIgNi0yIDYtMyA3LTEgNiAzIDEzIDIgMTAgMSAxMCAyIDEwIDEgMTEgMiAxMSAyIDE0IDEgNiAyIDExIDEgMTAgMSAxMmgxMGwxMS0xIDctMSA2LTIgMy0xIDMtMSAyLTEgNy0yIDI0LTE1IDIwLTE5di0xM2wxLTIycTAtMTAgMy05bDIgMSAxIDEgNCAyIDQgMSAyLTUgMy04IDEtNSAyLTYgMS02IDItOCA0LTM0LTEtMTYtMS0zLTItNy0zLTlxLTEtMy05LTEwaC0xM2wtMjkgMS00LTEtOS0zcS01LTItNy01bC0xLTdxMS00IDQtN2w5LTcgMTItOHE0LTIgMTctMTd2LTEwcTAtMTAtMi0xMmwtMS03IDEtNSAyLTQgNi01IDQtM3YtM2wtMTAtNC0xMS01cS00LTItMy02bDEtNSAzLTEgMS0xIDEtMSA1LTMgNS0yIDMtMSA2LTMgNC0xIDItMSAzLTMgMi0xMiAxLTEwIDEtNiA2LTM1IDEtNiAxLTggMi0xMCAxLTUgMS00IDEtNnExLTUgNS05bDUtNCAyLTEgOS0yIDEwLTFoNGwxLTIyYTEyMCAxMjAgMCAwIDEgNS0zNWwzLTcgNC00cTMgMCA2LTZsMi02IDEtMSAxLTEgNC03IDUtOCA3LTkgMTctMTRoM2wyLTJ6bS0yMCAxNS0xMCAxMC0yIDMtMiAyLTIgNC02IDEwLTMgNi0xIDItMiA1LTIgNi0xIDUtMyAxLTEtMi0xLTItMiAyLTMgMTEtMSAzOSAyLTYgMS01IDEtNSA0LTEwIDEtNSAyLTMgMi0zIDYtMTEgMTgtMjMgNi01LTEgMi00IDQtNCA1LTIgMy0yIDQtNCA1LTIgNS0yIDMtMSAyLTQgOS00IDExLTIgMy0xIDItMSA0LTMgOC0xIDctMSAzcTAgNCAxIDJsMTctMTcgNS01IDUtNy0xLTItMy0yIDMtNXEyLTUgNy03bDUtNSA5LTYgNC0zIDUtMyA0LTIgMi00aC00bC01LTFxLTMtMi0xIDFoLTFxLTItMy0xLTRsMi0xIDUtNCAxOC0yMCAxLTYtMy00LTgtMS04IDEtMiAxLTMgMi04IDUtMS0yIDItNSAzLTUgMi0zIDEtMS0yLTEtNCAyem0tNTcgMTI0LTEgMi0xIDcgMiAxMCA1IDUgNiAyIDYtMyA0LTcgMi00IDEtNi0xLTUtMS0zLTMtNHEtMi0zLTctMy00IDAtOCA0LTQgMy00IDVtLTY2IDE5IDEgMS0yIDEtMyAxLTUgMS01IDItMiAxLTUgMi03IDMtNSAxLTIgMS0zIDItMTAgNC04IDMtNiAzLTUgNC00IDItNSAyLTMgMi00IDMtMTAgNi03IDQtMSAxLTUgNC01IDMtMyAzLTkgOGE0NjUgNDY1IDAgMCAwLTUzIDUzbC0xLTJ2M2wtMSA0LTIgMy0xIDItNSA0LTMgNS00IDctNCA3LTEgMS0zIDZhNjU1IDY1NSAwIDAgMC0yOCA2N2wtMiA2LTEgNi0yIDYtMSA0LTUgMjNhNDgyIDQ4MiAwIDAgMCAxMiAxMzBsMSAyIDIgOCAyIDYgMiA0IDIgNiAxIDQgMiA0IDIgMyAxIDMgMSAyIDIgMyAxIDMgMiAzIDIgNSAyIDUgMiAzIDIgNCAxNyAyNCAyIDQgMyAzIDIgMyAzIDQgMTIgMTVhMjQyIDI0MiAwIDAgMCA0MCAzNmwzIDMgMyAyIDMgMyA2IDMgNCA0IDIgMiA0IDEgNSA0IDggNSA1MiAyNCA3IDIgMTAgNHYtMWwtNC0yLTktMy0yMS05LTQ0LTI0LTMtMi02LTQtMjgtMjItMjQtMjUtMTctMjAtMTgtMjYtMjEtMzktMi02LTEtNC0xLTMtMi00di00bC0xLTEtMS0yaDJsMSA1IDEgMiAyIDNxMCA0IDMgOGwyMyA0NCAzIDMgMyA1IDMgNCAzIDUgNCA1IDEgMSA0IDZhNzQ3IDc0NyAwIDAgMCA2MSA1NmwyMyAxNSA2NyAzMCA1IDIgNSAxIDYgMiA2IDEgNCAxIDMgMSA4IDEgOSAyIDE2IDJhMzkyIDM5MiAwIDAgMCA5MC01bDMtMSA0LTEgNi0xIDYtMiA1LTEgNC0yIDUtMSA1LTIgMTEtNCAxLTEgNC0xIDIwLTEwIDMtMiA1LTIgNS0yIDMtMiA0LTNhMjY4IDI2OCAwIDAgMCA1NC0zOWwyMy0yNSA3LTggOC0xMCA5LTEzIDktMTQgMy01IDQtNyAxNS0zMSAxLTMgMy02IDItNyAxLTMgMi03IDItNiAxIDItMSAzLTEgNS0yIDUtMSA1LTIgNS0xIDItMSA1LTM2IDY3LTIyIDMwYTI3MSAyNzEgMCAwIDEtNTMgNDZsLTIgMy05IDYtNyA0LTEgMS0yIDEtNyA3IDMtMiAyLTIgNC0xIDI5LTIwIDUtMyAxMS0xMCAzOS00MCAxOS0yNSA0LTcgMy00IDQtNyAyLTMgMi0zIDMtNyA4LTE2IDItMyAyLTUgMi01IDEtMiAzLTYgMy05IDEtNCAyLTYgMS01IDItNSAxLTYgMS00YTQxMyA0MTMgMCAwIDAgMy0xMjJsLTItOS0xLTUtMS0zLTEtNi0yLTYtMS01LTItNi00LTExYTI2NyAyNjcgMCAwIDAtMjYtNTVsLTI2LTM3LTQtNS0zMy0zNS0yMC0xOC0xLTJoLTR2LTJxMi0xLTEtMS0yIDItMi0xbC0yLTFxLTIgMC0xLTJsLTEtMWgtM3EtMiAwLTEtMmgtMmwtMS0xcTEtMS0xLTFsLTctNC01LTMtNi0zLTE1LTlxLTEwLTQtMTAtNmgzbDE1IDYgNDUgMjggMjEgMTZxNCA1IDYgNWwxMiAxMiAxNiAxOCAzMCAzOSAyIDUgMyA1IDEwIDE5IDQgNyA0IDExIDIgMyAxIDIgMSA0IDMgNyAxIDQgMiA0IDEgNSAxIDMgMSAzIDIgNiAxIDUgMiA3IDEgOCAyIDExIDMgMjQgMSAyOGEyMDkgMjA5IDAgMCAxLTQgNTFsLTExIDQ2LTIgNi0yIDUtMSAzLTQgMTAtNCAxMC0xIDMtMiAzLTEgMi0xIDMtMiAzLTQgNy0xMCAxNy0zIDUtNiA5LTEgMS0yIDMtMyA0LTcgOWE2MzIgNjMyIDAgMCAxLTUwIDUxbC01IDMtMiAxLTUgNS01IDMtMSAxLTMgMi0zNyAyMi00MyAxOC02IDItNyAzLTggMi03IDEtNyAyLTkgMi0zNSA1LTI0IDFhMTIxIDEyMSAwIDAgMS0zMi0ybC00OC05LTMtMS0zLTEtNS0yLTUtMS01LTEtNi0yLTktNGEyNDcgMjQ3IDAgMCAxLTU1LTI4bC00MC0yOWE0OTIgNDkyIDAgMCAxLTU2LTYybC0yLTQtMi0yLTQtNy01LTgtMy02LTMtNS0xLTEtNC05LTEtMS0zLTctMy05LTItMy0yLTQtMi02LTEtMy0xLTItMS01LTQtMTAtMi03LTEtNi0yLTUtMS04LTItOC0yLTEyYTQ3MyA0NzMgMCAwIDEgMy0xMDdsMi04IDItNyAxLTYgMS0zIDEtMyAxLTQgMi04IDItNCAxLTMgMS0yIDItNiAyLTQgMS0yIDItN2EzMzEgMzMxIDAgMCAxIDIyMC0xODRtODIgMTIwYTQ4MyA0ODMgMCAwIDEtNjUgM2wzIDcgMiAxMS0xIDYtMyAyLTQgMi0yIDItMiAzLTIgM3EwIDMgMSAybDQtNCAzLTQgMi0ycTItMiAxMS0ydDEwIDJsNCAycTMgMCA1IDZsMyA2IDIgNiA0IDEwIDIgNCA3IDEwIDEgMSAxIDUtMiA4LTMgMy01IDMtOCA0LTcgMi00IDEtMyAxLTMgMSAyIDIgMTAgMSAxNC0yIDgtNCAzLTIgNy03cTctNiAxMC0xMmExMTYgMTE2IDAgMCAwIDE1LTMybDEtMiAyLTQgMS01IDEtNCAxLTMgMi03IDEtNyAyLTdjMC02IDAtNi0zLThsLTUtNGgtOHptLTc5IDE3cS0zIDUtMSA4bDQgNnEyIDIgNi0xIDMgMCA0LTRsMS03cTAtNi0yLTdsLTUtMi0zIDJ6bTExNyAyLTMgMTMtMSA3LTIgNy0xIDQtMiA3LTIgNC0xIDItMSAyLTMgNy0xMCAxNi0zIDMtMyAzLTIgMy0zIDItMiAyLTkgNi0xMiA1LTQgMS05IDEtOSAxIDIgNSAyIDExYTE1NSAxNTUgMCAwIDEtMiA1MmwtMSA3LTIgNy0xIDMtNSAxOS0yIDUtMSAyIDIgMSAxIDIgMSAzIDIgOCAzIDE4cTIgMSA3LTExbDYtMTMgMi0yIDUtMiA2LTIgMzYtMTggMS0ydi0xOGE0MDMgNDAzIDAgMCAxIDktNzJsMS01IDEtNCAyLTQgMS01IDItNSAxLTQgMS0zIDEtMyAyLTQgMS00IDItNSAxLTZxMi0xIDItMjQgMC0yNC0yLTI0bC0xLTQtMS0yaC0xem0tMTAxIDIxYTIxMCAyMTAgMCAwIDEtNDggNDNsLTYgNXEtMiA0IDIgNiAyIDMgMyAybDIgMSAyIDEgMyAxaDE4bDItMSAzLTEgMy0yIDMtMSAzLTIgNS0yIDQtMiAyLTEgMi0xIDUtMiA3LTFxMi0xIDggMWw3IDItMSAycTAgMy00IDBsLTctMS00IDEtNCAxLTEwIDQtNiA0IDEwIDEgMTAtMSA3LTEgNy0yIDItMSA1LTJxNC0xIDYtNCAzLTIgMy03bC0yLTZxLTMtMi01LTZsLTgtMTgtNy0xMy00LTItMy0xLTQtMXEtNCAwLTcgM3ptLTE1OCA2OS00IDEtMyAxLTYgMy01IDMtNCAzLTYgOS0xIDItMiA0LTQgOS0xNCAzNS0zIDRxLTMgMi0xIDJ2MWwtMyAzLTggMTggMSA0IDctNCA3LTYgNi03IDctNiAxIDMtNSAxMy02IDE0LTIgMy0yIDMtMSA1LTIgNi0yIDlxLTEgNiAyIDVsNy00IDUtNyAyLTQgMy00IDQtNyAyLTYgMS0xIDMtN3EzLTcgNS03IDMtMSAyIDVsLTIgNS0xIDQtMSAyLTEgNS0yIDUtMiA0LTEgNi0yIDYtMiA5LTEgNS0xIDUgMSA1IDIgMiA0LTMgNC01IDItMyAxLTIgMS0xIDMtOCAxOS00MXY0OXExIDQgMyA0bDUtNCA0LTggMS00IDItMTQgMi02OS0xMC01My0zLTEtMiAxem0yNiA5IDEgNSAxIDUgMiAxMSA1IDMwIDEtMiAxLTMgMi02IDItMTQtMS0xMS0zLTgtNi04LTQtMy0yIDJ6bTIxMCAxNjgtNCA1cS0yIDQtNiA0bC05IDJhMzE2IDMxNiAwIDAgMS03OC0zbC0xIDEtMSAzcS0xIDQgMyAzbDMgMSA3IDEgNiAyIDkgMWEzOTkgMzk5IDAgMCAwIDY4LTJsMyAzIDcgMnE0IDAgNy00YzQtMyA0LTMgNC05cTAtNi00LThsLTYtM2gtNHptLTIxNSAyNy0yIDUtMSAzLTEgMTMgMSAxNyAyIDUgMSAzIDIgNiA2IDkgMTIgMTEgNiA0IDggMnE1IDEgOC0zbDQtNiAyLTE0di0xMGwtMTQtMTItNS0zLTQtMy0xNS03LTItMiAxLTJoM2w0IDEgOCAzYTEzNiAxMzYgMCAwIDEgMjcgMTlxNSA0IDUgMmwyLTQtMS0zLTEtMy0yLTYtMy03LTctOS0xMS0xMS0xMC01LTEwLTJxLTctMS05IDN6bTE5OCAzMC0xMCAyLTEyIDEtMTggMS00My02LTctMWgtM2wtMSA0cS0xIDMgNSA0bDcgMiAyIDEgNiAxIDggMiAyNSAxYTEyNCAxMjQgMCAwIDAgMzUtMmM3LTEgNy0xIDExIDJxNCAzIDcgM2w2LTEgNS0zIDEtOHExLTUtMS04bC01LTMtNi0xcS00IDAtOSA0em0tOSA0Ni01IDRhMzgyIDM4MiAwIDAgMS04MS0ybDEgNCA0IDIgNCAxIDcgMiA2IDEgNiAxaDQ5bDYtMXExLTIgNCAxbDQgMyAxIDEgNCAxIDctMSA0LTQgMS04LTEtNy02LTMtNS0yYTE2IDE2IDAgMCAwLTEwIDdtLTE2LTQ4NWExOCAxOCAwIDAgMC0yIDE1cTEgNCA1IDcgMyAzIDEwIDMgNiAwIDktM2wzLTUgMS0xIDEtNmMwLTYgMC03LTQtMTFxLTMtNS05LTUtNSAwLTkgMnptLTUwLTE0cTMgMCAyIDFsLTUgMi02IDEtNCAyLTUgMS01IDEtNCAyLTIgMS00IDEtMjYgMTItMzggMjItMjEgMTUtNCAzLTUgNC04IDctMjIgMjMtMTAgMTItMTggMjMtNiAxMC01IDctMTUgMjktMiA2LTEgMS0yIDQtMSA0LTEgMi0yIDUtMiA1LTEgNS0yIDUtMSAzdi0zbDEtNiAyLTUgMy05IDEtMyAyLTQgMi03IDEtNCAxLTEgMi01IDQtOSA0LTggNS0xMCA0LTcgOC0xMiA1LTggMjItMjYgOS0xMCA0LTQgMTQtMTMgNS0zIDE2LTEzIDQtMyA1LTMgNC0zIDEtMSA4LTQgMjMtMTIgMTAtNSA2LTIgMi0xIDEtMSA1LTIgNS0yIDUtMSA0LTIgNi0yIDYtMSA2LTIgNi0xem0yMDAgMTcgMyAyIDEzIDYgMiAxIDcgMyA3IDQgNiA0IDUgNCAyIDEgNiAzIDUgMyA0IDMgNSA0IDEgMSA3IDUgMjEgMjAgNDMgNTIgMiA1IDMgMyA0IDggNiAxNCA2IDEyYTI2OSAyNjkgMCAwIDEgMTkgNjNsMiA4IDEgOC0xIDUtMS00LTEtOC01LTIxLTEtMi0xLTItMi04LTYtMTgtMi01LTMtNWEzMTQgMzE0IDAgMCAwLTE1Ni0xNjVsLTMtMXYtM3pNNTE0IDQwM2w0IDEtMyAyLTMgMS0zIDEtMyAyLTMgNXEtMyAzIDAgN2wzIDZxMiAyIDggMnQ4LTNxNC0yIDMtOCAxLTUtMi04bC00LTNxLTIgMC0xLTJoM3E0IDAgNiA1IDMgNCAyIDEwbC0yIDYtMSAyLTUgNC03IDItMy0xLTUtMnEtMyAwLTUtNGwtMy03cS0xLTQgMi05IDEtNiA2LTd6bS03MCA1IDIgMnY0bC0yIDItNS00IDEtM3ptMTkgMzkgMiAzcTAgMy0yIDJsLTMgMWgtMWwxLTR6bTUzLTMzIDQgMSAxIDQtMyAzcS0zIDEtNC0xLTMtMi0xLTR6Ii8+PC9nPjwvc3ZnPg=="

const onboardingAssistantInstructions = `You are Goosar Helper, the built-in AI assistant for this Goosar workspace. Your role is to help any member use Goosar better — answer questions, give advice, and execute workspace operations on their behalf.

## What Goosar is

Goosar is an open-source, AI-native team workspace (source: https://github.com/adanman/goosar). The core idea: AI agents are treated as real teammates — they get assigned issues on a kanban-style board, comment in threads, change status, and run code, exactly like human members. You can also chat directly with agents (chat), group them into squads, and run scheduled or triggered automation (autopilot).

For concept details (workspace / issue / project / agent / runtime / skill / squad / autopilot / inbox / chat session): fetch https://goosar.ru/docs via WebFetch — that's authoritative. For the "why" or implementation, fetch the GitHub repo above. Never paraphrase concepts from memory.

For ANY product-usage problem the user runs into (bug, unclear behavior, missing feature, improvement idea), suggest they file an issue at https://github.com/adanman/goosar/issues — that's the official feedback channel.

## What you can do

Your toolbox is the ` + "`goosar`" + ` CLI. It's already on your PATH and authenticated as the workspace owner.

Your full capability surface = whatever ` + "`goosar --help`" + ` shows. Run ` + "`goosar --help`" + ` first, then ` + "`goosar <command> --help`" + ` for any subcommand; use ` + "`--output json`" + ` for structured data. The CLI is your manifest — never invent commands or flags.

A few things you can actually do (non-exhaustive — ` + "`--help`" + ` is the source of truth):
- Create issues, post comments
- Create or iterate on agents
- Manage projects, squads, autopilots, skills, runtimes, etc.

## Tone

Be concise and direct, like a colleague. Respond in the user's language (Chinese in, Chinese out). When pointing at a UI location, name the exact path ("Settings → Agents → New"); when pointing at a doc, link to the specific page, not the homepage. Never fabricate URLs, flags, or file paths.`

const onboardingIssueDescription = `Welcome to Goosar.

This is your guided first run. Goosar Helper is assigned to this issue and will help you try the core workflow:

1. Read Goosar Helper's first comment.
2. Reply with something you want to build, fix, write, or plan.
3. @mention Goosar Helper when you want it to continue.
4. Open Agents and Runtimes later when you want to customize the teammate or the computer it runs on.

You can close this issue when the workflow makes sense.`

type bootstrapOnboardingRuntimeRequest struct {
	WorkspaceID   string `json:"workspace_id"`
	RuntimeID     string `json:"runtime_id"`
	StarterPrompt string `json:"starter_prompt,omitempty"`
}

type bootstrapOnboardingRuntimeResponse struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	IssueID     string `json:"issue_id"`
}

type bootstrapOnboardingNoRuntimeRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type bootstrapOnboardingNoRuntimeResponse struct {
	WorkspaceID string `json:"workspace_id"`
	IssueID     string `json:"issue_id"`
}

func (h *Handler) BootstrapOnboardingRuntime(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, runtimeBootstrapBodyLimit)
	var req bootstrapOnboardingRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	req.StarterPrompt = strings.TrimSpace(req.StarterPrompt)
	if utf8.RuneCountInString(req.StarterPrompt) > maxStarterPromptLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("starter_prompt exceeds %d characters", maxStarterPromptLen))
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	req.WorkspaceID = uuidToString(wsUUID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start onboarding")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	member, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	runtime, err := qtx.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can create agents on it")
		return
	}

	agents, err := qtx.ListAgents(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	isFirstAgent := len(agents) == 0

	var assistant db.Agent
	assistantCreated := false
	for _, existing := range agents {
		if existing.Name == onboardingAssistantName && existing.Visibility == "workspace" {
			assistant = existing
			break
		}
	}
	if !assistant.ID.Valid {
		assistant, err = qtx.CreateAgent(r.Context(), db.CreateAgentParams{
			WorkspaceID:        wsUUID,
			Name:               onboardingAssistantName,
			Description:        onboardingAssistantDescription,
			AvatarUrl:          pgtype.Text{String: onboardingAssistantAvatarURL, Valid: true},
			RuntimeMode:        runtime.RuntimeMode,
			RuntimeConfig:      []byte("{}"),
			RuntimeID:          runtime.ID,
			Visibility:         "workspace",
			MaxConcurrentTasks: 6,
			OwnerID:            parseUUID(userID),
			Instructions:       onboardingAssistantInstructions,
			CustomEnv:          []byte("{}"),
			CustomArgs:         []byte("[]"),
			McpConfig:          nil,
			Model:              pgtype.Text{},
		})
		if err != nil {
			slog.Warn("bootstrap onboarding (shim): create assistant failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create onboarding assistant")
			return
		}
		assistantCreated = true
	}

	var emptyUUID pgtype.UUID
	issue, foundIssue, err := issueguard.LockAndFindActiveDuplicate(
		r.Context(), qtx, wsUUID, emptyUUID, emptyUUID, onboardingIssueTitle, false,
	)
	if err != nil {
		slog.Warn("bootstrap onboarding (shim): duplicate issue check failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
		return
	}
	issueCreated := false
	if !foundIssue {
		issueNumber, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to allocate issue number")
			return
		}
		description := onboardingIssueDescription
		if req.StarterPrompt != "" {
			description = req.StarterPrompt
		}
		issue, err = qtx.CreateIssue(r.Context(), db.CreateIssueParams{
			WorkspaceID:   wsUUID,
			Title:         onboardingIssueTitle,
			Description:   strOrNullText(description),
			Status:        "todo",
			Priority:      "high",
			AssigneeType:  pgtype.Text{String: "agent", Valid: true},
			AssigneeID:    assistant.ID,
			CreatorType:   "member",
			CreatorID:     parseUUID(userID),
			ParentIssueID: emptyUUID,
			Position:      0,
			Number:        issueNumber,
			ProjectID:     emptyUUID,
		})
		if err != nil {
			slog.Warn("bootstrap onboarding (shim): create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
			return
		}
		issueCreated = true
	}

	before, err := qtx.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	firstCompletion := !before.OnboardedAt.Valid
	updatedUser, err := qtx.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark onboarded")
		return
	}

	if err := claimStarterContentStateIfUnset(r.Context(), qtx, parseUUID(userID), before.StarterContentState); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record starter content state")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to finish onboarding")
		return
	}

	if assistantCreated {

		resp := broadcastAgentResponse(h.agentToResponse(assistant))
		h.publish(protocol.EventAgentCreated, req.WorkspaceID, "member", userID, map[string]any{"agent": resp})
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.AgentCreated(
			userID, req.WorkspaceID, uuidToString(assistant.ID),
			runtime.Provider, runtime.RuntimeMode, onboardingAgentTemplate, isFirstAgent,
		))
	}
	if issueCreated {
		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		resp := issueToResponse(issue, prefix)
		h.publish(protocol.EventIssueCreated, req.WorkspaceID, "member", userID, map[string]any{"issue": resp})
		platform, _, _ := middleware.ClientMetadataFromContext(r.Context())
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.IssueCreated(
			userID, req.WorkspaceID, uuidToString(issue.ID),
			uuidToString(assistant.ID), "", "", analytics.SourceOnboarding,
			platform,
		))
		if h.shouldEnqueueAgentTask(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
	}
	if firstCompletion {
		onboardedAt := ""
		if updatedUser.OnboardedAt.Valid {
			onboardedAt = updatedUser.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingCompleted(
			userID, req.WorkspaceID, analytics.OnboardingPathFull,
			onboardedAt, updatedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, bootstrapOnboardingRuntimeResponse{
		WorkspaceID: req.WorkspaceID,
		AgentID:     uuidToString(assistant.ID),
		IssueID:     uuidToString(issue.ID),
	})
}

func (h *Handler) BootstrapOnboardingNoRuntime(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, runtimeBootstrapBodyLimit)
	var req bootstrapOnboardingNoRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	req.WorkspaceID = uuidToString(wsUUID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start onboarding")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	userBefore, err := qtx.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	var emptyUUID pgtype.UUID
	existing, foundIssue, err := issueguard.LockAndFindActiveDuplicate(
		r.Context(), qtx, wsUUID, emptyUUID, emptyUUID, noRuntimeIssueTitle, false,
	)
	if err != nil {
		slog.Warn("bootstrap no-runtime onboarding (shim): duplicate issue check failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
		return
	}

	var issue db.Issue
	issueCreated := false
	if foundIssue {
		issue = existing
	} else {
		issueNumber, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to allocate issue number")
			return
		}
		issue, err = qtx.CreateIssue(r.Context(), db.CreateIssueParams{
			WorkspaceID:   wsUUID,
			Title:         noRuntimeIssueTitle,
			Description:   strOrNullText(noRuntimeIssueDescription(userBefore.Language)),
			Status:        "todo",
			Priority:      "high",
			AssigneeType:  pgtype.Text{String: "member", Valid: true},
			AssigneeID:    parseUUID(userID),
			CreatorType:   "member",
			CreatorID:     parseUUID(userID),
			ParentIssueID: emptyUUID,
			Position:      0,
			Number:        issueNumber,
			ProjectID:     emptyUUID,
		})
		if err != nil {
			slog.Warn("bootstrap no-runtime onboarding (shim): create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
			return
		}
		issueCreated = true
	}

	firstCompletion := !userBefore.OnboardedAt.Valid
	updatedUser, err := qtx.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark onboarded")
		return
	}
	if err := claimStarterContentStateIfUnset(r.Context(), qtx, parseUUID(userID), userBefore.StarterContentState); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record starter content state")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to finish onboarding")
		return
	}

	if issueCreated {
		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		resp := issueToResponse(issue, prefix)
		h.publish(protocol.EventIssueCreated, req.WorkspaceID, "member", userID, map[string]any{"issue": resp})
		platform2, _, _ := middleware.ClientMetadataFromContext(r.Context())
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.IssueCreated(
			userID, req.WorkspaceID, uuidToString(issue.ID),
			"", "", "", analytics.SourceOnboarding,
			platform2,
		))
	}
	if firstCompletion {
		onboardedAt := ""
		if updatedUser.OnboardedAt.Valid {
			onboardedAt = updatedUser.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingCompleted(
			userID, req.WorkspaceID, analytics.OnboardingPathRuntimeSkipped,
			onboardedAt, updatedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, bootstrapOnboardingNoRuntimeResponse{
		WorkspaceID: req.WorkspaceID,
		IssueID:     uuidToString(issue.ID),
	})
}

func noRuntimeIssueDescription(language pgtype.Text) string {
	tag := strings.ToLower(strings.TrimSpace(language.String))
	switch {
	case strings.HasPrefix(tag, "zh"):
		return zhNoRuntimeIssueDescription()
	case strings.HasPrefix(tag, "en"):
		return enNoRuntimeIssueDescription()
	default:
		return ruNoRuntimeIssueDescription()
	}
}

func enNoRuntimeIssueDescription() string {
	return strings.Join([]string{
		"Welcome to Goosar.",
		"",
		"Agents need a runtime before they can execute work. You can still use Goosar as a lightweight project-management workspace while you install one.",
		"",
		"## Try Goosar first",
		"",
		"Before the runtime is ready, you can:",
		"",
		"1. Create a project for your current work.",
		"2. Create a few issues and move them across backlog, todo, in_progress, and done.",
		"3. Add priorities, labels, comments, and subscriptions.",
		"4. Use Inbox to track assignments and mentions.",
		"",
		"That gives you the project-management layer first. Once a runtime is connected, agents can start working from the same issues.",
		"",
		"## Install your first agent runtime",
		"",
		"Full guide: https://goosar.ru/docs/install-agent-runtime",
		"",
		"For English users, the fastest first path is Codex:",
		"",
		"1. Make sure Node.js is installed.",
		"2. Install Codex:",
		"   npm i -g @openai/codex",
		"3. Sign in:",
		"   codex",
		"4. Confirm your terminal can find it:",
		"   which codex",
		"   codex --version",
		"5. Restart the Goosar daemon:",
		"   goosar daemon restart",
		"   If you use the desktop app, restarting the app is enough.",
		"6. Return to Runtimes and refresh. You should see a Codex runtime online.",
		"7. Create your first agent from that runtime, then assign an issue to the agent and set status to todo.",
		"",
		"Codex reference: https://developers.openai.com/codex/cli",
		"",
		"When the runtime is connected, you can create Goosar Helper for a guided first run.",
	}, "\n")
}

func zhNoRuntimeIssueDescription() string {
	return strings.Join([]string{
		"欢迎来到 Goosar。",
		"",
		"智能体需要先连上运行时才能执行工作。运行时还没准备好时，你也可以先把 Goosar 当作轻量项目管理工具体验起来。",
		"",
		"## 先体验项目管理功能",
		"",
		"运行时安装前，你可以先做这些事：",
		"",
		"1. 为当前工作创建一个项目。",
		"2. 新建几个 issue，并在 backlog、todo、in_progress、done 之间流转。",
		"3. 给 issue 加优先级、标签、评论和订阅。",
		"4. 用收件箱追踪分配给你的事项和 @mention。",
		"",
		"这样你先熟悉项目管理层。连上运行时后，智能体会直接在这些 issue 上开始工作。",
		"",
		"## 安装第一个 Agent 运行时",
		"",
		"完整文档：https://goosar.ru/docs/install-agent-runtime",
		"",
		"中文用户建议先装 Kimi CLI：",
		"",
		"1. 在 macOS / Linux 终端安装 Kimi CLI：",
		"   curl -LsSf https://code.kimi.com/install.sh | bash",
		"   Windows PowerShell：",
		"   Invoke-RestMethod https://code.kimi.com/install.ps1 | Invoke-Expression",
		"2. 确认终端能找到 Kimi：",
		"   kimi --version",
		"3. 在你想让 Kimi 工作的项目目录里启动一次：",
		"   kimi",
		"4. 首次启动后输入 /login，按提示完成 Kimi Code 或 API key 配置。",
		"5. 重启 Goosar 守护进程：",
		"   goosar daemon restart",
		"   如果你用桌面端，重启 app 即可。",
		"6. 回到 Runtimes 页面刷新。你应该能看到一个在线的 Kimi 运行时。",
		"7. 用这个运行时创建第一个智能体，再把一个 issue 分配给它，并把状态切到 todo。",
		"",
		"Kimi CLI 官方文档：https://moonshotai.github.io/kimi-cli/zh/guides/getting-started.html",
		"",
		"运行时连上后，你就可以创建 Goosar Helper，开始一次有智能体参与的上手引导。",
	}, "\n")
}

func ruNoRuntimeIssueDescription() string {
	return strings.Join([]string{
		"Добро пожаловать в Goosar.",
		"",
		"Агентам нужна среда выполнения, чтобы выполнять работу. Пока вы её устанавливаете, Goosar можно использовать как лёгкий инструмент управления проектами.",
		"",
		"## Сначала осмотритесь в Goosar",
		"",
		"Пока среда выполнения не готова, можно:",
		"",
		"1. Создать проект для текущей работы.",
		"2. Создать несколько issue и провести их через backlog, todo, in_progress и done.",
		"3. Добавить приоритеты, метки, комментарии и подписки.",
		"4. Отслеживать назначения и упоминания во «Входящих».",
		"",
		"Так вы сначала освоите слой управления проектами. Когда среда выполнения подключится, агенты начнут работать с теми же issue.",
		"",
		"## Установите первую среду выполнения для агентов",
		"",
		"Полное руководство: https://goosar.ru/docs/install-agent-runtime",
		"",
		"Самый быстрый первый путь — Codex:",
		"",
		"1. Убедитесь, что установлен Node.js.",
		"2. Установите Codex:",
		"   npm i -g @openai/codex",
		"3. Войдите в аккаунт:",
		"   codex",
		"4. Проверьте, что терминал его находит:",
		"   which codex",
		"   codex --version",
		"5. Перезапустите демон Goosar:",
		"   goosar daemon restart",
		"   Если вы пользуетесь десктопным приложением, достаточно перезапустить его.",
		"6. Вернитесь в «Среды выполнения» и обновите страницу. Там должна появиться среда выполнения Codex со статусом online.",
		"7. Создайте из неё первого агента, затем назначьте ему issue и поставьте статус todo.",
		"",
		"Справочник Codex: https://developers.openai.com/codex/cli",
		"",
		"Когда среда выполнения подключена, создайте Goosar Helper — он проведёт вас через первый запуск.",
	}, "\n")
}

func strOrNullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func claimStarterContentStateIfUnset(
	ctx context.Context,
	q *db.Queries,
	userID pgtype.UUID,
	current pgtype.Text,
) error {
	if current.Valid {
		return nil
	}
	_, err := q.SetStarterContentState(ctx, db.SetStarterContentStateParams{
		ID:                  userID,
		StarterContentState: pgtype.Text{String: "imported", Valid: true},
	})
	return err
}
