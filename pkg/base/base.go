package base

import "regexp"

const ScriptHausVersion = "0.4.0"
const ScHomeVarName = "SCRIPTHAUS_HOME"
const HomeVarName = "HOME"
const DBFileName = "scripthaus.db"
const CurDBVersion = 1
const ScPathVarName = "SCRIPTHAUS_PATH"

var PlaybookPrefixRe = regexp.MustCompile("^(\\^|[.]*)(?:[a-zA-Z_]|$)")
var PlaybookFileNameRe = regexp.MustCompile("^[a-zA-Z_][a-zA-Z0-9_-]*[.]md$")
var PlaybookScriptNameRe = regexp.MustCompile("^[a-zA-Z_][a-zA-Z0-9_/-]*$")

const RunTypePlaybook = "playbook"
const RunTypeScript = "script"
