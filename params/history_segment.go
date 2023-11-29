package params

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/common"
)

const (
	BoundStartBlock      uint64 = 31268530 // The starting block height of the first segment, was produced on Aug-29-2023
	HistorySegmentLength uint64 = 2592000  // Assume 1 block for every 3 second, 2,592,000 blocks will be produced in 90 days.
)

var (
	historySegmentsInBSCMainnet = unmarshalHisSegments(`
[
    {
        "index": 0,
        "start_at_block": {
            "number": 0,
            "hash": "0x0d21840abff46b96c84b2ac9e10e4f5cdaeb5693cb665db62a2f3b02d2d57b5b",
            "td": 0
        },
        "finality_at_block": {
            "number": 0,
            "hash": "0x0000000000000000000000000000000000000000000000000000000000000000",
            "td": 0
        }
    },
    {
        "index": 1,
        "start_at_block": {
            "number": 31268530,
            "hash": "0xdb8a505f19ef04cb21ae79e3cb641963ffc44f3666e6fde499be55a72b6c7865",
            "td": 62131329,
            "consensus_data": "eyJudW1iZXIiOjMxMjY4NTMwLCJoYXNoIjoiMHhkYjhhNTA1ZjE5ZWYwNGNiMjFhZTc5ZTNjYjY0MTk2M2ZmYzQ0ZjM2NjZlNmZkZTQ5OWJlNTVhNzJiNmM3ODY1IiwidmFsaWRhdG9ycyI6eyIweDBiYWM0OTIzODY4NjJhZDNkZjRiNjY2YmMwOTZiMDUwNWJiNjk0ZGEiOnsiaW5kZXg6b21pdGVtcHR5IjoxLCJ2b3RlX2FkZHJlc3MiOlsxNzYsMTkwLDE5NSw3MiwxMDQsMjYsMjQ3LDEwMiwxMTcsMjgsMTg0LDU3LDg3LDExMCwxNTYsODEsOTAsOSwyMDAsMTkxLDI1MCw0OCwxNjQsOTgsMTUwLDIwNCwxOTcsMTAyLDE4LDczLDE0LDE4MCwxMjgsMjA4LDU5LDI0OSw3MiwyMjUsMCw1LDE4NywyMDQsNCwzMywyNDksMTEsNjEsNzhdfSwiMHgyNDY1MTc2YzQ2MWFmYjMxNmViYzc3M2M2MWZhZWU4NWE2NTE1ZGFhIjp7ImluZGV4Om9taXRlbXB0eSI6Miwidm90ZV9hZGRyZXNzIjpbMTM4LDE0Niw1MywxMDAsMTk4LDI1NSwyMTEsMTI3LDE3OCwyNTQsMTU5LDE3LDE0MiwyNDgsMTI4LDE0NiwyMzIsMTE4LDQ0LDEyMiwyMjEsMTgxLDM4LDE3MSwxMjYsMTc3LDIzMSwxMTQsMTg2LDIzOSwxMzMsMjQsMzEsMTM3LDQ0LDExNSwyNywyMjQsMTkzLDEzNywyNiw4MCwyMzAsMTc2LDk4LDk4LDIwMCwyMl19LCIweDJkNGM0MDdiYmU0OTQzOGVkODU5ZmU5NjViMTQwZGNmMWFhYjcxYTkiOnsiaW5kZXg6b21pdGVtcHR5IjozLCJ2b3RlX2FkZHJlc3MiOlsxNDcsMTkzLDI0NywyNDYsMTQ2LDE1NywzMSwyMjYsMTYxLDEyMyw3OCwyMCw5Nyw3OCwyNDksMjUyLDkxLDIyMCwxMTMsNjEsMTAyLDQ5LDIxNCwxMTcsNjQsNjMsMTkwLDIzOSwxNzIsODUsOTcsMjcsMjQ2LDE4LDExMiwxMSwyNywxMDEsMjQ0LDExNiw3Miw5NywxODQsMTEsMTUsMTI1LDEwNiwxNzZdfSwiMHgzZjM0OWJiYWZlYzE1NTE4MTliOGJlMWVmZWEyZmM0NmNhNzQ5YWExIjp7ImluZGV4Om9taXRlbXB0eSI6NCwidm90ZV9hZGRyZXNzIjpbMTMyLDM2LDEzOCw2OSwxNDgsMTAwLDIzOCwxOTMsMTYyLDMwLDEyNywxOTksMTgzLDI2LDUsNjEsMTUwLDY4LDIzMywxODcsMTQxLDE2NCwxMzMsNTksMTQzLDEzNSw0NCwyMTUsMTkzLDIxNCwxNzksMzYsMTkxLDI1LDM0LDEzMCwxNTIsNDgsMTAwLDEwOCwyMzQsMjIzLDE4Miw4OCwyMTEsMjIyLDAsMTU0XX0sIjB4NjFkZDQ4MWExMTRhMmU3NjFjNTU0YjY0MTc0MmM5NzM4Njc4OTlkMyI6eyJpbmRleDpvbWl0ZW1wdHkiOjUsInZvdGVfYWRkcmVzcyI6WzEzOCwxMjgsMTUwLDEyNSw1NywyMjgsNiwxNjAsMTY5LDEwMCw0NSw2NSwyMzMsMCwxMjIsMzksMjUyLDE3LDgwLDE2MiwxMDMsMjA5LDY3LDE2OSwyNDcsMTM0LDIwNSw0Myw5NCwyMzYsMTg5LDIwNCw2NCw1NCwzOSw1NSw1LDM0LDkxLDE0OSwxMDksOTQsNDcsMTQzLDk0LDE4NSw5MywzN119LCIweDY4NWIxZGVkODAxMzc4NWQ2NjIzY2MxOGQyMTQzMjBiNmJiNjQ3NTkiOnsiaW5kZXg6b21pdGVtcHR5Ijo2LCJ2b3RlX2FkZHJlc3MiOlsxMzgsOTYsMjQ4LDQyLDEyMywyMDcsMTE2LDE4MCwyMDMsNSw1OSwxNTUsMjU0LDEzMSwyMDgsMjM3LDIsMTY4LDc4LDE4NywxNiwxMzQsOTMsMjUzLDIxNiwyMjYsMTEwLDExNyw1MywxOTYsNTgsMjgsMjA0LDIxMCwxMDQsMjMyLDk2LDI0NSwyLDMzLDEwNyw1NSwxNTcsMjUyLDE1MywxMTMsMjExLDg4XX0sIjB4NzBmNjU3MTY0ZTViNzU2ODliNjRiN2ZkMWZhMjc1ZjMzNGYyOGUxOCI6eyJpbmRleDpvbWl0ZW1wdHkiOjcsInZvdGVfYWRkcmVzcyI6WzE1MCwxNjIsMTA2LDI1MCwxOCwxNDksMjE4LDEyOSw2NSwxMzMsMTQ3LDE4OSwxOCwxMjksNjgsOTksMjE3LDI0NiwyMjgsOTIsNTQsMTYwLDIyOCwxMjYsMTgwLDIwNSw2Miw5MSwxMDYsMjQyLDE1Niw2NSwyMjYsMTYzLDE2NSw5OSwxMDAsNDgsMjEsOTAsNzAsMTEwLDMzLDEwMSwxMzMsMTc1LDU5LDE2N119LCIweDcyYjYxYzYwMTQzNDJkOTE0NDcwZWM3YWMyOTc1YmUzNDU3OTZjMmIiOnsiaW5kZXg6b21pdGVtcHR5Ijo4LCJ2b3RlX2FkZHJlc3MiOlsxMjksMjE5LDQsMzQsMTY1LDI1Myw4LDIyOCwxMywxNzcsMjUyLDM1LDEwNCwyMTAsMzYsOTQsNzUsMjQsMTc3LDIwOCwxODQsOTIsMTQ2LDI2LDE3MCwxNzUsMjEwLDIyNyw2NSwxMTgsMTQsNDEsMjUyLDk3LDYyLDIyMSw1NywyNDcsMTgsODQsOTcsNzgsMzIsODUsMTk1LDQwLDEyMiw4MV19LCIweDdhZTJmNWI5ZTM4NmNkMWI1MGE0NTUwNjk2ZDk1N2NiNDkwMGYwM2EiOnsiaW5kZXg6b21pdGVtcHR5Ijo5LCJ2b3RlX2FkZHJlc3MiOlsxODQsNzksMTMxLDI1NSw0NSwyNDQsNjUsMTQ3LDczLDEwMywxNDcsMTg0LDcxLDI0Niw3OCwxNTcsMTA5LDE3NywxNzksMTQ5LDU0LDEzMCwxODcsMTQ5LDIzNywyMDgsMTUwLDIzNSwzMCwxMDUsMTg3LDIxMSw4NywxOTQsMCwxNTMsNDQsMTY3LDEyOCw4MCwyMDgsMjAzLDIyNSwxMjgsMjA3LDE3MCwxLDE0Ml19LCIweDhiNmM4ZmQ5M2Q2ZjRjZWE0MmJiYjM0NWRiYzZmMGRmZGI1YmVjNzMiOnsiaW5kZXg6b21pdGVtcHR5IjoxMCwidm90ZV9hZGRyZXNzIjpbMTY4LDE2Miw4Nyw3LDc4LDEzMCwxODQsMTI5LDIwNywxNjAsMTEwLDI0MywyMzUsNzgsMjU0LDIwMiw2LDEyLDM3LDQ5LDUzLDE1NCwxODksMTQsMTcxLDEzOCwyNDEsMjI3LDIzNywyNTAsMzIsMzcsMjUyLDE2NCwxMDAsMTcyLDE1Niw2MywyMDksMzUsMjQ2LDE5NCw3NCwxMywxMjAsMTM0LDE0OCwxMzNdfSwiMHhhNmY3OWI2MDM1OWYxNDFkZjkwYTBjNzQ1MTI1YjEzMWNhYWZmZDEyIjp7ImluZGV4Om9taXRlbXB0eSI6MTEsInZvdGVfYWRkcmVzcyI6WzE4MywxMTQsMjI1LDEyOCwyNTEsMjQzLDEzOCw1LDI4LDE1MSwyMTgsMTg4LDEzOCwxNzAsMSwzOCwxNjIsNTEsMTY5LDIzMiw0MCwyMDUsMTc1LDIwNCwxMTYsMzQsMTk2LDE4NywzMSw2NCw0OCwxNjUsMTA3LDE2MywxMDAsMTk3LDY1LDMsMjQyLDEwNywxNzMsMTQ1LDgwLDEzOSw4MiwzMiwxODMsNjVdfSwiMHhiMjE4YzVkNmFmMWY5NzlhYzQyYmM2OGQ5OGE1YTBkNzk2YzZhYjAxIjp7ImluZGV4Om9taXRlbXB0eSI6MTIsInZvdGVfYWRkcmVzcyI6WzE4Miw4OSwxNzMsMTUsMTg5LDE1OSw4MSw4OCwxNDcsMjUzLDIxNSw2NCwxNzgsMTU1LDE2MCwxMTksNDUsMTg5LDIzMywxODAsOTksODksMzMsMjIxLDE0NSwxODksNDEsOTksMTYwLDI1MiwxMzMsOTQsNDksMjQ2LDUxLDE0Myw2OSwxNzgsMTcsMTk2LDIzMywyMjIsMjE5LDEyNyw0NiwxNzYsMTU3LDIzMV19LCIweGI0ZGQ2NmQ3YzJjN2U1N2Y2MjgyMTAxODcxOTJmYjg5ZDRiOTlkZDQiOnsiaW5kZXg6b21pdGVtcHR5IjoxMywidm90ZV9hZGRyZXNzIjpbMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDBdfSwiMHhiZTgwN2RkZGIwNzQ2MzljZDlmYTYxYjQ3Njc2YzA2NGZjNTBkNjJjIjp7ImluZGV4Om9taXRlbXB0eSI6MTQsInZvdGVfYWRkcmVzcyI6WzE3NywyNDIsMTk5LDIxLDExOSwyMjIsMjQzLDIwLDc5LDE3MSwyMzUsMTE3LDE2OCwxNjEsMjAwLDIwMyw5MSw4MSwyMDksMjA5LDE4MCwxNjAsOTQsMjM2LDEwMywxNTIsMTM5LDEzNCwxMzMsMCwxMzksMTcwLDIzLDY5LDE1OCwxOTYsMzcsMjE5LDE3NCwxODgsMTMzLDQ3LDczLDEwOSwyMDEsMzMsMTUwLDIwNV19LCIweGNjOGU2ZDAwYzE3ZWI0MzEzNTBjNmM1MGQ4YjhmMDUxNzZiOTBiMTEiOnsiaW5kZXg6b21pdGVtcHR5IjoxNSwidm90ZV9hZGRyZXNzIjpbMTc5LDE2MywyMTIsMjU0LDE4NCwzNywxNzQsMTUxLDIsMTEzLDIxLDEwMiwyMjMsOTMsMTkxLDU2LDIzMiw0MiwyMjEsNzcsMjA5LDE4MSwxMTUsMTg1LDkzLDM2LDEwMiwyNTAsMTAxLDEsMjA0LDE4NCwzMCwxNTcsMzgsMTYzLDgyLDE4NSw5Nyw4MCwyMDQsMTkxLDEyMywxMDUsMTI3LDIwOCwxNjQsMjVdfSwiMHhjZTJmZDc1NDRlMGIyY2M5NDY5MmQ0YTcwNGRlYmVmN2JjYjYxMzI4Ijp7ImluZGV4Om9taXRlbXB0eSI6MTYsInZvdGVfYWRkcmVzcyI6WzE4Miw3NCwxOTAsMzcsOTcsNzYsMTU2LDI1Myw1MCwyMjgsODYsMTgwLDIxMywzMywyNDIsMTU2LDEzMSw4NywyNDQsMTc1LDcwLDYsMTUxLDEzMCwxNTAsMjAxLDE5MCwxNDcsNzMsNjQsMTE0LDE3Miw1LDI1MCwxMzQsMjI3LDIxMCwxMjQsMjAwLDIxNCwxMTAsMTAxLDAsMTUsMTM5LDE2Myw2MywxODddfSwiMHhlMmQzYTczOWVmZmNkM2E5OTM4N2QwMTVlMjYwZWVmYWM3MmViZWExIjp7ImluZGV4Om9taXRlbXB0eSI6MTcsInZvdGVfYWRkcmVzcyI6WzE0OSwxMDgsNzEsMTMsMjIzLDI0NCwxNDAsMTgwLDE0NywwLDMyLDExLDk1LDEzMSw3MywxMjcsNTgsNjAsMjAzLDU4LDIzNSwxMzEsMTk3LDIzNywyMTcsMTI5LDEzMywxMDUsMywxNDIsOTcsMjA5LDE1MSwyNCw3OSw3NCwxNjYsMTQ3LDE1OCwxNjUsMjMzLDE0NSwzMCw2MiwxNTIsMTcyLDEwOSwzM119LCIweGU5YWUzMjYxYTQ3NWEyN2JiMTAyOGYxNDBiYzJhN2M4NDMzMThhZmQiOnsiaW5kZXg6b21pdGVtcHR5IjoxOCwidm90ZV9hZGRyZXNzIjpbMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDBdfSwiMHhlYTBhNmUzYzUxMWJiZDEwZjQ1MTllY2UzN2RjMjQ4ODdlMTFiNTVkIjp7ImluZGV4Om9taXRlbXB0eSI6MTksInZvdGVfYWRkcmVzcyI6WzE3OCwyMTIsMTk4LDQwLDYwLDY4LDE2MSwxOTksMTg5LDgwLDU4LDE3MSwxNjcsMTAyLDExMCwxNTksMTIsMTMxLDE0LDE1LDI0MCwyMiwxOTMsMTk5LDgwLDE2NSwyMjgsMTM1LDg3LDE2NywxOSwyMDgsMTMxLDEwNywyOCwxNzEsMjUzLDkyLDQwLDI3LDI5LDIyNywxODMsMTI1LDI4LDI1LDMzLDEzMV19LCIweGVlMjI2Mzc5ZGI4M2NmZmM2ODE0OTU3MzBjMTFmZGRlNzliYTRjMGMiOnsiaW5kZXg6b21pdGVtcHR5IjoyMCwidm90ZV9hZGRyZXNzIjpbMTc0LDEyMywxOTgsMjUwLDE2MywyNDAsMjA0LDYyLDk2LDE0NywxODIsNTEsMjUzLDEyNiwyMjgsMjQ4LDEwNSwxMTIsMTQ2LDEwNSw4OCwyMDgsMTgzLDIzNiwxMjgsNjcsMTI3LDE0NywxMDYsMjA3LDMzLDQzLDEyMCwyNDAsMjA1LDksOTUsNjksMTAxLDI1NSwyNDEsNjgsMjUzLDY5LDE0MSwzNSw1OCw5MV19LCIweGVmMDI3NGUzMTgxMGM5ZGYwMmY5OGZhZmRlMGY4NDFmNGU2NmExY2QiOnsiaW5kZXg6b21pdGVtcHR5IjoyMSwidm90ZV9hZGRyZXNzIjpbMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDBdfX0sInJlY2VudHMiOnsiMzEyNjg1MjAiOiIweDNmMzQ5YmJhZmVjMTU1MTgxOWI4YmUxZWZlYTJmYzQ2Y2E3NDlhYTEiLCIzMTI2ODUyMSI6IjB4NjFkZDQ4MWExMTRhMmU3NjFjNTU0YjY0MTc0MmM5NzM4Njc4OTlkMyIsIjMxMjY4NTIyIjoiMHg2ODViMWRlZDgwMTM3ODVkNjYyM2NjMThkMjE0MzIwYjZiYjY0NzU5IiwiMzEyNjg1MjMiOiIweDcwZjY1NzE2NGU1Yjc1Njg5YjY0YjdmZDFmYTI3NWYzMzRmMjhlMTgiLCIzMTI2ODUyNCI6IjB4NzJiNjFjNjAxNDM0MmQ5MTQ0NzBlYzdhYzI5NzViZTM0NTc5NmMyYiIsIjMxMjY4NTI1IjoiMHg3YWUyZjViOWUzODZjZDFiNTBhNDU1MDY5NmQ5NTdjYjQ5MDBmMDNhIiwiMzEyNjg1MjYiOiIweDhiNmM4ZmQ5M2Q2ZjRjZWE0MmJiYjM0NWRiYzZmMGRmZGI1YmVjNzMiLCIzMTI2ODUyNyI6IjB4YTZmNzliNjAzNTlmMTQxZGY5MGEwYzc0NTEyNWIxMzFjYWFmZmQxMiIsIjMxMjY4NTI4IjoiMHhiMjE4YzVkNmFmMWY5NzlhYzQyYmM2OGQ5OGE1YTBkNzk2YzZhYjAxIiwiMzEyNjg1MjkiOiIweGI0ZGQ2NmQ3YzJjN2U1N2Y2MjgyMTAxODcxOTJmYjg5ZDRiOTlkZDQiLCIzMTI2ODUzMCI6IjB4YmU4MDdkZGRiMDc0NjM5Y2Q5ZmE2MWI0NzY3NmMwNjRmYzUwZDYyYyJ9LCJyZWNlbnRfZm9ya19oYXNoZXMiOnsiMzEyNjg1MTAiOiJiMTlkZjRhMiIsIjMxMjY4NTExIjoiYjE5ZGY0YTIiLCIzMTI2ODUxMiI6ImIxOWRmNGEyIiwiMzEyNjg1MTMiOiJiMTlkZjRhMiIsIjMxMjY4NTE0IjoiYjE5ZGY0YTIiLCIzMTI2ODUxNSI6ImIxOWRmNGEyIiwiMzEyNjg1MTYiOiJiMTlkZjRhMiIsIjMxMjY4NTE3IjoiYjE5ZGY0YTIiLCIzMTI2ODUxOCI6ImIxOWRmNGEyIiwiMzEyNjg1MTkiOiJiMTlkZjRhMiIsIjMxMjY4NTIwIjoiYjE5ZGY0YTIiLCIzMTI2ODUyMSI6ImIxOWRmNGEyIiwiMzEyNjg1MjIiOiJiMTlkZjRhMiIsIjMxMjY4NTIzIjoiYjE5ZGY0YTIiLCIzMTI2ODUyNCI6ImIxOWRmNGEyIiwiMzEyNjg1MjUiOiJiMTlkZjRhMiIsIjMxMjY4NTI2IjoiYjE5ZGY0YTIiLCIzMTI2ODUyNyI6ImIxOWRmNGEyIiwiMzEyNjg1MjgiOiJiMTlkZjRhMiIsIjMxMjY4NTI5IjoiYjE5ZGY0YTIiLCIzMTI2ODUzMCI6ImIxOWRmNGEyIn0sImF0dGVzdGF0aW9uOm9taXRlbXB0eSI6eyJTb3VyY2VOdW1iZXIiOjMxMjY4NTI4LCJTb3VyY2VIYXNoIjoiMHg1NTc4MGUwMmMyYTE0MjFkZWZmODhmYzIzMmRmMzZiM2EwNjdmNmQ0ZDVkYjhmZGFjYTI4YjQxYmNmNjg0YmZiIiwiVGFyZ2V0TnVtYmVyIjozMTI2ODUyOSwiVGFyZ2V0SGFzaCI6IjB4OTg4ODA1MjYzMTZlMDU3ZWFiOTA4MWE3OWViNDJjYTZjYjhlN2E3YzA4MDZkMTIyYTlhMmY1OGViNTQ3YzcwNSJ9fQ=="
        },
        "finality_at_block": {
            "number": 31268532,
            "hash": "0xaa1b4e4d251289d21da95e66cf9b57f641b2dbc8031a2bb145ae58ee7ade03e7",
            "td": 62131333
        }
    }
]`)
	historySegmentsInBSCChapel = unmarshalHisSegments(`
[
    {
        "index": 0,
        "start_at_block": {
            "number": 0,
            "hash": "0x6d3c66c5357ec91d5c43af47e234a939b22557cbb552dc45bebbceeed90fbe34",
            "td": 0
        },
        "finality_at_block": {
            "number": 0,
            "hash": "0x0000000000000000000000000000000000000000000000000000000000000000",
            "td": 0
        }
    },
    {
        "index": 1,
        "start_at_block": {
            "number": 31268530,
            "hash": "0x2ab32e1541202ac43f3dc9ff80b998002ad9130ecc24c40a1f00a8e45dc1f786",
            "td": 62348266,
            "consensus_data": "eyJudW1iZXIiOjMxMjY4NTMwLCJoYXNoIjoiMHgyYWIzMmUxNTQxMjAyYWM0M2YzZGM5ZmY4MGI5OTgwMDJhZDkxMzBlY2MyNGM0MGExZjAwYThlNDVkYzFmNzg2IiwidmFsaWRhdG9ycyI6eyIweDEyODQyMTRiOWI5Yzg1NTQ5YWIzZDJiOTcyZGYwZGVlZjY2YWMyYzkiOnsiaW5kZXg6b21pdGVtcHR5IjoxLCJ2b3RlX2FkZHJlc3MiOlsxNDIsMTMwLDE0Nyw3NiwxNjksMTE2LDI1MywyMDUsMTUxLDI0Myw0OCwxNTcsMjMzLDEwMywyMTEsMjAxLDE5Niw2MywxNjcsMTcsMTY4LDIxNCwxMTUsMTc1LDkzLDExNyw3MCw4OCw2OCwxOTEsMTM3LDEwNSwyMDAsMjA5LDE0OCwxNDEsMTQ0LDU1LDcyLDE3MiwxMjMsMTM5LDIzLDMyLDI1MCwxMDAsMjI5LDEyXX0sIjB4MzU1NTJjMTY3MDRkMjE0MzQ3ZjI5ZmE3N2Y3N2RhNmQ3NWQ3Yzc1MiI6eyJpbmRleDpvbWl0ZW1wdHkiOjIsInZvdGVfYWRkcmVzcyI6WzE4Myw2NiwxNzMsNzIsODUsMTg2LDIyNyw0OCw2NiwxMDcsMTMwLDYyLDExNiw0NSwxNjMsMzEsMTI5LDEwOCwyMDAsNTksMTkzLDEwOSwxMDUsMTY5LDE5LDc1LDIyNCwyMDcsMTgwLDE2MSwyMDksMTI2LDE5NSw3OSwyNyw5MSw1MCwyMTMsMTk0LDQsNjQsMTg0LDgzLDEwNywzMCwxMzYsMjQwLDI0Ml19LCIweDk4MGE3NWVjZDEzMDllYTEyZmEyZWQ4N2E4NzQ0ZmJmYzliODYzZDUiOnsiaW5kZXg6b21pdGVtcHR5IjozLCJ2b3RlX2FkZHJlc3MiOlsxMzcsMywxMjIsMTU0LDIwNiw1OSw4OSwxLDEwMSwyMzQsMjgsMTIsOTAsMTk5LDQzLDI0NiwwLDE4MywyMDAsMTQwLDMwLDY3LDk1LDY1LDE0Nyw0NCwxNyw1MCwxNzAsMjI1LDE5MSwxNjAsMTg3LDEwNCwyMjgsMTA3LDE1MCwyMDQsMTc3LDQ0LDUyLDIxLDIyOCwyMTYsNDIsMjQ3LDIzLDIxNl19LCIweGEyOTU5ZDNmOTVlYWU1ZGM3ZDcwMTQ0Y2UxYjczYjQwM2I3ZWI2ZTAiOnsiaW5kZXg6b21pdGVtcHR5Ijo0LCJ2b3RlX2FkZHJlc3MiOlsxODUsMTE1LDE5NCwyMTEsMTMyLDEzNSwyMjksMTQzLDIxNCwyMjUsNjksNzMsMjcsMTcsMCwxMjgsMjUxLDIwLDE3MiwxNDUsOTAsNCwxNywyNTIsMTIwLDI0MSwxNTgsOSwxNjMsMTUzLDIyMSwyMzgsMTMsMzIsMTk4LDU4LDExNywyMTYsMjQ5LDQ4LDI0MSwxMDUsNjksNjgsMTczLDQ1LDE5MiwyN119LCIweGI3MWIyMTRjYjg4NTUwMDg0NDM2NWU5NWNkOTk0MmM3Mjc2ZTdmZDgiOnsiaW5kZXg6b21pdGVtcHR5Ijo1LCJ2b3RlX2FkZHJlc3MiOlsxNjIsMTE3LDE0LDE5OCwyMjEsMjM3LDYxLDIwNSwxOTQsMjQzLDgxLDEyMCwzNSwxNiwxNzYsMjM0LDIyMCw3LDEyNSwxODEsMTU0LDE4OCwxNjAsMjQwLDIwNSwzOCwxMTksMTEwLDQ2LDEyMiwyMDMsMTU5LDU5LDIwNiw2NCwxNzcsMjUwLDgyLDMzLDI1MywyMSw5NywzNCwxMDgsOTgsOTksMjA0LDk1XX0sIjB4ZjQ3NGNmMDNjY2VmZjI4YWJjNjVjOWNiYWU1OTRmNzI1YzgwZTEyZCI6eyJpbmRleDpvbWl0ZW1wdHkiOjYsInZvdGVfYWRkcmVzcyI6WzE1MCwyMDEsMTg0LDEwOCw1MiwwLDIyOSw0MSwxOTEsMjI1LDEzMiw1LDExMCwzNywxMjQsNywxNDgsMTEsMTgyLDEwMCw5OSwxMTEsMTA0LDE1OCwxNDEsMzIsMzksMjAwLDUyLDEwNCwzMSwxNDMsMTM1LDEzOSwxMTUsNjgsODIsOTcsMyw3OCwxNDgsMTA3LDE3OCwyMTcsMSwxODAsMTg0LDEyMF19fSwicmVjZW50cyI6eyIzMTI2ODUyNyI6IjB4MzU1NTJjMTY3MDRkMjE0MzQ3ZjI5ZmE3N2Y3N2RhNmQ3NWQ3Yzc1MiIsIjMxMjY4NTI4IjoiMHg5ODBhNzVlY2QxMzA5ZWExMmZhMmVkODdhODc0NGZiZmM5Yjg2M2Q1IiwiMzEyNjg1MjkiOiIweGEyOTU5ZDNmOTVlYWU1ZGM3ZDcwMTQ0Y2UxYjczYjQwM2I3ZWI2ZTAiLCIzMTI2ODUzMCI6IjB4YjcxYjIxNGNiODg1NTAwODQ0MzY1ZTk1Y2Q5OTQyYzcyNzZlN2ZkOCJ9LCJyZWNlbnRfZm9ya19oYXNoZXMiOnsiMzEyNjg1MjUiOiJkYzU1OTA1YyIsIjMxMjY4NTI2IjoiZGM1NTkwNWMiLCIzMTI2ODUyNyI6ImRjNTU5MDVjIiwiMzEyNjg1MjgiOiJkYzU1OTA1YyIsIjMxMjY4NTI5IjoiZGM1NTkwNWMiLCIzMTI2ODUzMCI6ImRjNTU5MDVjIn0sImF0dGVzdGF0aW9uOm9taXRlbXB0eSI6eyJTb3VyY2VOdW1iZXIiOjMxMjY4NTI4LCJTb3VyY2VIYXNoIjoiMHgxMzZjNWI0YzZmODdmOWVkYzQ0NDI5NTkzNzY3YTNhOTg5MWJiM2MxNTZiNzM5OWRlYzRmNjIyMDcwOWQwYjU1IiwiVGFyZ2V0TnVtYmVyIjozMTI2ODUyOSwiVGFyZ2V0SGFzaCI6IjB4YzQyODQxNjYzODBhYzk0OGQzMzhmYzU1ZGI2ZWEyY2E4NWViMDllZDdkYTAwM2QzYTVhNDEzNzMyMjlhZDkwZCJ9fQ=="
        },
        "finality_at_block": {
            "number": 31268532,
            "hash": "0x59203b593d2e4c213e65f68db2c19309380416a93592aa8f923d59aebc481c28",
            "td": 62348270
        }
    },
    {
        "index": 2,
        "start_at_block": {
            "number": 33860530,
            "hash": "0x252e966e2420ecb2c5c51da62f147ac89004943e2b76c343bb1b2d8465f29a29",
            "td": 67529251,
            "consensus_data": "eyJudW1iZXIiOjMzODYwNTMwLCJoYXNoIjoiMHgyNTJlOTY2ZTI0MjBlY2IyYzVjNTFkYTYyZjE0N2FjODkwMDQ5NDNlMmI3NmMzNDNiYjFiMmQ4NDY1ZjI5YTI5IiwidmFsaWRhdG9ycyI6eyIweDEyODQyMTRiOWI5Yzg1NTQ5YWIzZDJiOTcyZGYwZGVlZjY2YWMyYzkiOnsiaW5kZXg6b21pdGVtcHR5IjoxLCJ2b3RlX2FkZHJlc3MiOlsxNDIsMTMwLDE0Nyw3NiwxNjksMTE2LDI1MywyMDUsMTUxLDI0Myw0OCwxNTcsMjMzLDEwMywyMTEsMjAxLDE5Niw2MywxNjcsMTcsMTY4LDIxNCwxMTUsMTc1LDkzLDExNyw3MCw4OCw2OCwxOTEsMTM3LDEwNSwyMDAsMjA5LDE0OCwxNDEsMTQ0LDU1LDcyLDE3MiwxMjMsMTM5LDIzLDMyLDI1MCwxMDAsMjI5LDEyXX0sIjB4MzU1NTJjMTY3MDRkMjE0MzQ3ZjI5ZmE3N2Y3N2RhNmQ3NWQ3Yzc1MiI6eyJpbmRleDpvbWl0ZW1wdHkiOjIsInZvdGVfYWRkcmVzcyI6WzE4Myw2NiwxNzMsNzIsODUsMTg2LDIyNyw0OCw2NiwxMDcsMTMwLDYyLDExNiw0NSwxNjMsMzEsMTI5LDEwOCwyMDAsNTksMTkzLDEwOSwxMDUsMTY5LDE5LDc1LDIyNCwyMDcsMTgwLDE2MSwyMDksMTI2LDE5NSw3OSwyNyw5MSw1MCwyMTMsMTk0LDQsNjQsMTg0LDgzLDEwNywzMCwxMzYsMjQwLDI0Ml19LCIweDQ3Nzg4Mzg2ZDBlZDZjNzQ4ZTAzYTUzMTYwYjRiMzBlZDM3NDhjYzUiOnsiaW5kZXg6b21pdGVtcHR5IjozLCJ2b3RlX2FkZHJlc3MiOlswLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMCwwLDAsMF19LCIweDk4MGE3NWVjZDEzMDllYTEyZmEyZWQ4N2E4NzQ0ZmJmYzliODYzZDUiOnsiaW5kZXg6b21pdGVtcHR5Ijo0LCJ2b3RlX2FkZHJlc3MiOlsxMzcsMywxMjIsMTU0LDIwNiw1OSw4OSwxLDEwMSwyMzQsMjgsMTIsOTAsMTk5LDQzLDI0NiwwLDE4MywyMDAsMTQwLDMwLDY3LDk1LDY1LDE0Nyw0NCwxNyw1MCwxNzAsMjI1LDE5MSwxNjAsMTg3LDEwNCwyMjgsMTA3LDE1MCwyMDQsMTc3LDQ0LDUyLDIxLDIyOCwyMTYsNDIsMjQ3LDIzLDIxNl19LCIweGEyOTU5ZDNmOTVlYWU1ZGM3ZDcwMTQ0Y2UxYjczYjQwM2I3ZWI2ZTAiOnsiaW5kZXg6b21pdGVtcHR5Ijo1LCJ2b3RlX2FkZHJlc3MiOlsxODUsMTE1LDE5NCwyMTEsMTMyLDEzNSwyMjksMTQzLDIxNCwyMjUsNjksNzMsMjcsMTcsMCwxMjgsMjUxLDIwLDE3MiwxNDUsOTAsNCwxNywyNTIsMTIwLDI0MSwxNTgsOSwxNjMsMTUzLDIyMSwyMzgsMTMsMzIsMTk4LDU4LDExNywyMTYsMjQ5LDQ4LDI0MSwxMDUsNjksNjgsMTczLDQ1LDE5MiwyN119LCIweGI3MWIyMTRjYjg4NTUwMDg0NDM2NWU5NWNkOTk0MmM3Mjc2ZTdmZDgiOnsiaW5kZXg6b21pdGVtcHR5Ijo2LCJ2b3RlX2FkZHJlc3MiOlsxNjIsMTE3LDE0LDE5OCwyMjEsMjM3LDYxLDIwNSwxOTQsMjQzLDgxLDEyMCwzNSwxNiwxNzYsMjM0LDIyMCw3LDEyNSwxODEsMTU0LDE4OCwxNjAsMjQwLDIwNSwzOCwxMTksMTEwLDQ2LDEyMiwyMDMsMTU5LDU5LDIwNiw2NCwxNzcsMjUwLDgyLDMzLDI1MywyMSw5NywzNCwxMDgsOTgsOTksMjA0LDk1XX0sIjB4ZjQ3NGNmMDNjY2VmZjI4YWJjNjVjOWNiYWU1OTRmNzI1YzgwZTEyZCI6eyJpbmRleDpvbWl0ZW1wdHkiOjcsInZvdGVfYWRkcmVzcyI6WzE1MCwyMDEsMTg0LDEwOCw1MiwwLDIyOSw0MSwxOTEsMjI1LDEzMiw1LDExMCwzNywxMjQsNywxNDgsMTEsMTgyLDEwMCw5OSwxMTEsMTA0LDE1OCwxNDEsMzIsMzksMjAwLDUyLDEwNCwzMSwxNDMsMTM1LDEzOSwxMTUsNjgsODIsOTcsMyw3OCwxNDgsMTA3LDE3OCwyMTcsMSwxODAsMTg0LDEyMF19fSwicmVjZW50cyI6eyIzMzg2MDUyNyI6IjB4MzU1NTJjMTY3MDRkMjE0MzQ3ZjI5ZmE3N2Y3N2RhNmQ3NWQ3Yzc1MiIsIjMzODYwNTI4IjoiMHg0Nzc4ODM4NmQwZWQ2Yzc0OGUwM2E1MzE2MGI0YjMwZWQzNzQ4Y2M1IiwiMzM4NjA1MjkiOiIweDk4MGE3NWVjZDEzMDllYTEyZmEyZWQ4N2E4NzQ0ZmJmYzliODYzZDUiLCIzMzg2MDUzMCI6IjB4YTI5NTlkM2Y5NWVhZTVkYzdkNzAxNDRjZTFiNzNiNDAzYjdlYjZlMCJ9LCJyZWNlbnRfZm9ya19oYXNoZXMiOnsiMzM4NjA1MjQiOiJkYzU1OTA1YyIsIjMzODYwNTI1IjoiZGM1NTkwNWMiLCIzMzg2MDUyNiI6ImRjNTU5MDVjIiwiMzM4NjA1MjciOiJkYzU1OTA1YyIsIjMzODYwNTI4IjoiZGM1NTkwNWMiLCIzMzg2MDUyOSI6ImRjNTU5MDVjIiwiMzM4NjA1MzAiOiJkYzU1OTA1YyJ9LCJhdHRlc3RhdGlvbjpvbWl0ZW1wdHkiOnsiU291cmNlTnVtYmVyIjozMzg2MDUyOCwiU291cmNlSGFzaCI6IjB4MGUwNmFhZmMzODU2ZWE2ZDg3OTA1MGJlMWIwNGQ0MjgwNDc1OTkyNDRkZDY2OTE5NzdiMDkwZTA1ZDM0ZTNjZSIsIlRhcmdldE51bWJlciI6MzM4NjA1MjksIlRhcmdldEhhc2giOiIweGFlYzc4ZDlhZjRiYjk0Y2VmNjFiMmU1MDg2OWQwYTVjNzMzZGFiNzI3ZDE1NDcwYjkyMzA2ZmNmNThjMTEzMmUifX0="
        },
        "finality_at_block": {
            "number": 33860532,
            "hash": "0x424e526d901ae91897340655c81db7de16428a3322df4fa712693bda83572f8f",
            "td": 67529255
        }
    }
]`)
	historySegmentsInBSCRialto = []HisSegment{
		{
			Index: 0,
			StartAtBlock: HisBlockInfo{
				Number: 0,
				Hash:   RialtoGenesisHash,
			},
		},
	}
)

type HisBlockInfo struct {
	Number        uint64      `json:"number"`
	Hash          common.Hash `json:"hash"`
	TD            uint64      `json:"td"`
	ConsensusData []byte      `json:"consensus_data,omitempty"` // add consensus data, like parlia snapshot
}

func (b *HisBlockInfo) Equals(c *HisBlockInfo) bool {
	if b == nil || c == nil {
		return b == c
	}
	if b.Number != c.Number {
		return false
	}
	if b.Hash != c.Hash {
		return false
	}
	if b.TD != c.TD {
		return false
	}
	if !bytes.Equal(b.ConsensusData, c.ConsensusData) {
		return false
	}
	return true
}

type HisSegment struct {
	Index           uint64       `json:"index"`             // segment index number
	StartAtBlock    HisBlockInfo `json:"start_at_block"`    // target segment start from here
	FinalityAtBlock HisBlockInfo `json:"finality_at_block"` // the StartAtBlock finality's block
	// TODO(0xbundler): if need add more finality evidence? like signature?
}

func (s *HisSegment) String() string {
	return fmt.Sprintf("[Index: %v, StartAt: %v, FinalityAt: %v]", s.Index, s.StartAtBlock, s.FinalityAtBlock)
}

func (s *HisSegment) MatchBlock(h common.Hash, n uint64) bool {
	if s.StartAtBlock.Number == n && s.StartAtBlock.Hash == h {
		return true
	}
	return false
}

func (s *HisSegment) Equals(compared *HisSegment) bool {
	if s == nil || compared == nil {
		return s == compared
	}
	if s.Index != compared.Index {
		return false
	}
	if !s.StartAtBlock.Equals(&compared.StartAtBlock) {
		return false
	}
	if !s.FinalityAtBlock.Equals(&compared.FinalityAtBlock) {
		return false
	}
	return true
}

type HistorySegmentConfig struct {
	CustomPath string      // custom HistorySegments file path, need read from the file
	Genesis    common.Hash // specific chain genesis, it may use hard-code config
}

func (cfg *HistorySegmentConfig) LoadCustomSegments() ([]HisSegment, error) {
	if _, err := os.Stat(cfg.CustomPath); err != nil {
		return nil, err
	}
	enc, err := os.ReadFile(cfg.CustomPath)
	if err != nil {
		return nil, err
	}
	var ret []HisSegment
	if err = json.Unmarshal(enc, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

type HistorySegmentManager struct {
	segments []HisSegment
	cfg      *HistorySegmentConfig
}

func NewHistorySegmentManager(cfg *HistorySegmentConfig) (*HistorySegmentManager, error) {
	if cfg == nil {
		return nil, errors.New("cannot init HistorySegmentManager by nil config")
	}

	// if genesis is one of the hard code history segment, just ignore input custom file
	var (
		segments []HisSegment
		err      error
	)
	switch cfg.Genesis {
	case BSCGenesisHash:
		segments = historySegmentsInBSCMainnet
	// TODO(0xbundler): temporary got testing
	//case ChapelGenesisHash:
	//	segments = historySegmentsInBSCChapel
	//case RialtoGenesisHash:
	//	segments = historySegmentsInBSCRialto
	default:
		segments, err = cfg.LoadCustomSegments()
		if err != nil {
			return nil, err
		}
	}
	if err = ValidateHisSegments(cfg.Genesis, segments); err != nil {
		return nil, err
	}
	return &HistorySegmentManager{
		segments: segments,
		cfg:      cfg,
	}, nil
}

func ValidateHisSegments(genesis common.Hash, segments []HisSegment) error {
	if len(segments) == 0 {
		return errors.New("history segment length cannot be 0")
	}
	expectSeg0 := HisSegment{
		Index: 0,
		StartAtBlock: HisBlockInfo{
			Number: 0,
			Hash:   genesis,
		},
	}
	if !segments[0].Equals(&expectSeg0) {
		return fmt.Errorf("wrong segement0 start block, it must be genesis, expect: %v, actual: %v", expectSeg0, segments[0])
	}
	for i := 1; i < len(segments); i++ {
		if segments[i].Index != uint64(i) ||
			segments[i].StartAtBlock.Number <= segments[i-1].StartAtBlock.Number ||
			segments[i].StartAtBlock.Number+2 > segments[i].FinalityAtBlock.Number {
			return fmt.Errorf("wrong segement, index: %v, segment: %v", i, segments[i])
		}
	}

	return nil
}

// HisSegments return all history segments
func (m *HistorySegmentManager) HisSegments() []HisSegment {
	return m.segments
}

// CurSegment return which segment include this block
func (m *HistorySegmentManager) CurSegment(num uint64) HisSegment {
	segments := m.HisSegments()
	i := len(segments) - 1
	for i >= 0 {
		if segments[i].StartAtBlock.Number <= num {
			break
		}
		i--
	}
	return segments[i]
}

// LastSegment return the current's last segment, because the latest 2 segments is available,
// so user could keep current & prev segment
func (m *HistorySegmentManager) LastSegment(cur HisSegment) (HisSegment, bool) {
	segments := m.HisSegments()
	if cur.Index == 0 || cur.Index >= uint64(len(segments)) {
		return HisSegment{}, false
	}
	return segments[cur.Index-1], true
}

// LastSegmentByNumber return the current's last segment
func (m *HistorySegmentManager) LastSegmentByNumber(num uint64) (HisSegment, bool) {
	cur := m.CurSegment(num)
	return m.LastSegment(cur)
}

func unmarshalHisSegments(enc string) []HisSegment {
	var ret []HisSegment
	err := json.Unmarshal([]byte(enc), &ret)
	if err != nil {
		panic(err)
	}
	return ret
}
