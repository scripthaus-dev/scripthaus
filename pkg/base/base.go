package base

import "regexp"

const ScriptHausVersion = "0.5.1"
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

func ValidScriptTypes() []string {
	return []string{"sh", "zsh", "tcsh", "bash", "ksh", "fish", "python", "python2", "python3", "js", "node"}
}

func IsValidScriptType(scriptType string) bool {
	switch scriptType {
	case "sh", "bash", "zsh", "tcsh", "ksh", "fish":
		return true

	case "python", "python2", "python3":
		return true

	case "js", "node":
		return true

	default:
		return false
	}
}

func GetCommentString(scriptType string) string {
	if scriptType == "js" || scriptType == "node" {
		return "//"
	}
	return "#"
}
