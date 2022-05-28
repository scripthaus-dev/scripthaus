package history

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/user"
	"path"
	"strconv"
	"time"

	"github.com/alessio/shellescape"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/scripthaus-dev/scripthaus/pkg/base"
	"github.com/scripthaus-dev/scripthaus/pkg/pathutil"
)

const VersionMdKey = "version"

var createDBSql string = `
CREATE TABLE scripthaus_meta (
    name varchar(30) PRIMARY KEY,
    value text
);

CREATE TABLE history (
    historyid integer PRIMARY KEY,
    ts integer,
    scversion text,
    runtype text,
    scriptpath text,
    scriptfile text,
    scriptname text,
    scripttype text,
    metadata text,
    cwd text,
    hostname text,
    ipaddr text,
    sysuser text,
    cmdline text,
    durationms int,
    exitcode int
);

INSERT INTO scripthaus_meta (name, value) VALUES ('version', '1');
`

type HistoryQuery struct {
	ShowAll bool
	ShowNum int
}

type HistoryItem struct {
	HistoryId  int64
	Ts         int64
	ScVersion  string
	RunType    string
	ScriptPath string
	ScriptFile string
	ScriptName string
	ScriptType string
	Metadata   string
	Cwd        string
	HostName   string
	IpAddr     string
	SysUser    string
	CmdLine    string
	DurationMs sql.NullInt64 // update
	ExitCode   sql.NullInt64 // update
}

func (item *HistoryItem) MarshalJSON() ([]byte, error) {
	jm := make(map[string]interface{})
	jm["historyid"] = item.HistoryId
	jm["ts"] = item.Ts
	jm["date"] = time.UnixMilli(item.Ts).Format("2006-01-02T15:04:05")
	jm["version"] = item.ScVersion
	jm["runtype"] = item.RunType
	jm["scriptpath"] = item.ScriptPath
	jm["scriptfile"] = item.ScriptFile
	jm["scriptname"] = item.ScriptName
	jm["scripttype"] = item.ScriptType
	jm["cwd"] = item.Cwd
	jm["hostname"] = item.HostName
	jm["ipaddr"] = item.IpAddr
	jm["sysuser"] = item.SysUser
	jm["cmdline"] = item.CmdLine
	if item.DurationMs.Valid {
		jm["durationms"] = item.DurationMs
	}
	if item.ExitCode.Valid {
		jm["exitcode"] = item.ExitCode
	}
	return json.Marshal(jm)
}

func (item *HistoryItem) DecodeCmdLine() []string {
	if item.CmdLine == "" {
		return nil
	}
	var rtn []string
	err := json.Unmarshal([]byte(item.CmdLine), &rtn)
	if err != nil {
		return nil
	}
	return rtn
}

func (item *HistoryItem) CompactString() string {
	return fmt.Sprintf("%5d  %s %s\n", item.HistoryId, item.ScriptString(), shellescape.QuoteCommand(item.DecodeCmdLine()))
}

func (item *HistoryItem) ScriptString() string {
	if item.RunType == base.RunTypePlaybook {
		return fmt.Sprintf("%s/%s", item.ScriptFile, item.ScriptName)
	} else {
		return item.ScriptFile
	}
}

func (item *HistoryItem) FullString() string {
	tsStr := time.UnixMilli(item.Ts).Format("[2006-01-02 15:04:05]")
	line1 := fmt.Sprintf("%5d  %s %s %s\n", item.HistoryId, tsStr, item.ScriptString(), shellescape.QuoteCommand(item.DecodeCmdLine()))
	line2 := fmt.Sprintf("       cwd: %s", item.Cwd)
	if item.DurationMs.Valid {
		line2 += fmt.Sprintf(" | duration: %0.3fms", float64(item.DurationMs.Int64)/1000)
	}
	if item.ExitCode.Valid {
		line2 += fmt.Sprintf(" | exitcode: %d", item.ExitCode.Int64)
	}
	line2 += "\n"
	line3 := fmt.Sprintf("       user: %s | host: %s | ip: %s\n", item.SysUser, item.HostName, item.IpAddr)
	return line1 + line2 + line3 + "\n"
}

func (item *HistoryItem) EncodeCmdLine(args []string) {
	item.CmdLine = marshalJsonNoErr(args)
}

func ReNumberHistory() error {
	sqlStr := `
        DROP TABLE IF EXISTS temp.history_renum;

        CREATE TEMPORARY TABLE history_renum AS
        SELECT historyid + 1000000000 as oldhid, row_number() over (order by ts) as newhid
        FROM history;

        UPDATE history
        SET historyid = historyid + 1000000000;

        UPDATE history
        SET historyid = (SELECT renum.newhid FROM history_renum renum WHERE renum.oldhid = history.historyid);
`
	db, err := getDBConn()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Beginx()
	if err != nil {
		return fmt.Errorf("cannot start transaction (for history re-numbering): %w", err)
	}
	_, err = tx.Exec(sqlStr)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("cannot execute history re-numbering: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("cannot commit history re-numbering: %w", err)
	}
	return nil
}

// returns (numRemoved, error)
func RemoveHistoryItems(removeAll bool, startId int, endId int) (int, error) {
	if startId < 0 || endId < 0 {
		return 0, fmt.Errorf("invalid ids passed to scripthaus manage remove-history-range %d %d, both indexes must be positive", startId, endId)
	}
	if endId < startId {
		return 0, nil
	}
	sqlStr := `DELETE FROM history`
	if !removeAll {
		sqlStr = fmt.Sprintf("%s WHERE historyid >= %d AND historyid <= %d", sqlStr, startId, endId)
	}
	db, err := getDBConn()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	result, err := db.Exec(sqlStr)
	if err != nil {
		return 0, fmt.Errorf("cannot remove history items: %w", err)
	}
	numRemoved, err := result.RowsAffected()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: history items removed, but error getting number of rows affected: %v", err)
	}
	return int(numRemoved), nil
}

func InsertHistoryItem(item *HistoryItem) error {
	sqlStr := `
        INSERT INTO history 
            (historyid, ts, scversion, runtype, scriptpath, scriptfile, scriptname, scripttype, metadata, cwd, hostname, ipaddr, sysuser, cmdline)
        VALUES 
            (NULL, :ts, :scversion, :runtype, :scriptpath, :scriptfile, :scriptname, :scripttype, :metadata, :cwd, :hostname, :ipaddr, :sysuser, :cmdline)
`
	db, err := getDBConn()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.NamedExec(sqlStr, item)
	if err != nil {
		return fmt.Errorf("cannot insert into db: %w", err)
	}
	return nil
}

func UpdateHistoryItem(item *HistoryItem) error {
	sqlStr := `
        UPDATE history
        SET durationms = :durationms,
            exitcode = :exitcode
        WHERE ts = :ts
`
	db, err := getDBConn()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.NamedExec(sqlStr, item)
	if err != nil {
		return fmt.Errorf("cannot update db: %w", err)
	}
	return nil
}

func marshalJsonNoErr(val interface{}) string {
	var jsonBuf bytes.Buffer
	enc := json.NewEncoder(&jsonBuf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(val)
	if err != nil {
		return ""
	}
	rtn := jsonBuf.Bytes()
	if len(rtn) > 0 && rtn[len(rtn)-1] == '\n' {
		rtn = rtn[:len(rtn)-1]
	}
	return string(rtn)
}

func GetLocalIpAddr() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	udpAddr := conn.LocalAddr().(*net.UDPAddr)
	return udpAddr.IP.String()
}

func BuildHistoryItem() *HistoryItem {
	var rtn HistoryItem
	rtn.Ts = time.Now().UnixMilli()
	rtn.ScVersion = base.ScriptHausVersion
	rtn.Cwd, _ = os.Getwd()
	rtn.HostName, _ = os.Hostname()
	rtn.IpAddr = GetLocalIpAddr()
	osUser, _ := user.Current()
	if osUser != nil {
		rtn.SysUser = osUser.Username
	}
	return &rtn
}

func wrapFsErr(fileType string, fileName string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s '%s' does not exist", fileType, fileName)
	}
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%s '%s' has invalid permissions: %w", fileType, fileName, err)
	}
	return fmt.Errorf("cannot stat %s '%s': %w", fileType, fileName, err)
}

func dbConnStr(fileName string, createOk bool) string {
	modeStr := "rw"
	if createOk {
		modeStr = "rwc"
	}
	return fmt.Sprintf("file:%s?cache=shared&mode=%s", fileName, modeStr)
}

func DebugDBFileError() error {
	dbFileName, err := GetHistoryDBFileName()
	if err != nil {
		return err
	}
	dirName := path.Dir(dbFileName)
	dirInfo, err := os.Stat(dirName)
	if err != nil {
		return wrapFsErr("scripthaus home directory", dirName, err)
	}
	if !dirInfo.IsDir() {
		return fmt.Errorf("scripthaus home directory '%s' is not a directory", dirName)
	}
	fileInfo, err := os.Stat(dbFileName)
	if err != nil {
		return wrapFsErr("scripthaus history db", dbFileName, err)
	}
	if fileInfo.IsDir() {
		return fmt.Errorf("scripthaus history db '%s' is a directory (not a file)", dbFileName)
	}
	db, err := sqlx.Connect("sqlite3", dbConnStr(dbFileName, false))
	if err != nil {
		return fmt.Errorf("error opening scripthaus history db '%s': %w", dbFileName, err)
	}
	defer db.Close()
	return nil
}

func GetHistoryDBFileName() (string, error) {
	scHome, err := pathutil.GetScHomeDir()
	if err != nil {
		return "", err
	}
	return path.Join(scHome, base.DBFileName), nil
}

func RemoveDB() error {
	dbFileName, err := GetHistoryDBFileName()
	if err != nil {
		return err
	}
	err = os.Remove(dbFileName)
	if err != nil {
		return fmt.Errorf("cannot remove scripthaus db file '%s': %v", dbFileName, err)
	}
	return nil
}

func createDB() error {
	scHomeDir, err := pathutil.GetScHomeDir()
	if err != nil {
		return fmt.Errorf("cannot create history db: %w", err)
	}
	homeDirFinfo, err := os.Stat(scHomeDir)
	if errors.Is(err, fs.ErrNotExist) {
		err = os.MkdirAll(scHomeDir, 0777)
		if err != nil {
			return fmt.Errorf("cannot create scripthaus home directory '%s': %w", scHomeDir, err)
		}
		fmt.Fprintf(os.Stderr, "[^scripthaus] created scripthaus home directory at '%s'\n", scHomeDir)
	} else if err != nil {
		return fmt.Errorf("cannot stat scripthaus home directory '%s': %w", scHomeDir, err)
	} else if !homeDirFinfo.IsDir() {
		return fmt.Errorf("invalid scripthaus home directory '%s' is a file (not a directory)", scHomeDir)
	}
	dbFileName, err := GetHistoryDBFileName()
	if err != nil {
		return fmt.Errorf("cannot create history db in home directory '%s': %w", scHomeDir, err)
	}
	db, err := sqlx.Connect("sqlite3", dbConnStr(dbFileName, true))
	if err != nil {
		return fmt.Errorf("cannot create history db '%s': %w", dbFileName, err)
	}
	_, err = db.Exec(createDBSql)
	if err != nil {
		return fmt.Errorf("cannot create history db '%s': %w", dbFileName, err)
	}
	fmt.Fprintf(os.Stderr, "[^scripthaus] created scripthaus history db at '%s'\n", dbFileName)
	return nil
}

func upgradeDB(db *sqlx.DB, curVersion int) error {
	if curVersion == base.CurDBVersion {
		return nil
	}
	if curVersion > base.CurDBVersion {
		return fmt.Errorf("cannot use history db, version is too high, currentversion=%d, required=%d", curVersion, base.CurDBVersion)
	}
	return nil
}

type metadataRow struct {
	Name  string
	Value string
}

func readMetadata(db *sqlx.DB) (map[string]string, error) {
	var anyVal interface{}
	qstr := `SELECT name FROM sqlite_master WHERE type='table' AND name='scripthaus_meta';`
	err := db.Get(&anyVal, qstr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error querying history db: %w", err)
	}
	qstr = `SELECT name, value FROM scripthaus_meta`
	rows, err := db.Queryx(qstr)
	if err != nil {
		return nil, fmt.Errorf("error reading history db scripthaus_meta: %w", err)
	}
	metadataMap := make(map[string]string)
	for rows.Next() {
		var mrow metadataRow
		err = rows.StructScan(&mrow)
		if err != nil {
			return nil, fmt.Errorf("error reading history db scripthaus_meta: %w", err)
		}
		metadataMap[mrow.Name] = mrow.Value
	}
	return metadataMap, nil
}

func getMetadataVersion(md map[string]string) int {
	versionStr := md[VersionMdKey]
	if versionStr == "" {
		return 0
	}
	versionNum, _ := strconv.Atoi(versionStr)
	return versionNum
}

func checkUpgradeDB(db *sqlx.DB) error {
	md, err := readMetadata(db)
	if err != nil {
		return err
	}
	versionNum := getMetadataVersion(md)
	if versionNum == base.CurDBVersion {
		return nil
	}
	err = upgradeDB(db, versionNum)
	if err != nil {
		return err
	}
	md, err = readMetadata(db)
	if err != nil {
		return err
	}
	versionNum = getMetadataVersion(md)
	if versionNum != base.CurDBVersion {
		return fmt.Errorf("upgrade of history db failed, dbversion=%d, requiredversion=%d", versionNum, base.CurDBVersion)
	}
	return nil
}

func getDBConn() (*sqlx.DB, error) {
	dbFileName, err := GetHistoryDBFileName()
	if err != nil {
		return nil, err
	}
	_, err = os.Stat(dbFileName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			err = createDB()
			if err != nil {
				return nil, err
			}
		}
	}
	db, err := sqlx.Connect("sqlite3", dbConnStr(dbFileName, false))
	if err != nil {
		return nil, fmt.Errorf("error opening scripthaus history db '%s': %w", dbFileName, err)
	}
	err = checkUpgradeDB(db)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func reverseHistorySlice(arr []*HistoryItem) {
	for i, j := 0, len(arr)-1; i < j; i, j = i+1, j-1 {
		arr[i], arr[j] = arr[j], arr[i]
	}
}

func QueryHistory(query HistoryQuery) ([]*HistoryItem, error) {
	sqlStr := `
        SELECT * FROM history
        WHERE TRUE
        ORDER BY ts DESC
`
	if !query.ShowAll {
		limit := 50
		if query.ShowNum > 0 {
			limit = query.ShowNum
		}
		sqlStr = sqlStr + " " + fmt.Sprintf("LIMIT %d", limit)
	}
	var rtn []*HistoryItem
	db, err := getDBConn()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Queryx(sqlStr)
	if err != nil {
		return nil, fmt.Errorf("cannot query history db: %w", err)
	}
	for rows.Next() {
		item := &HistoryItem{}
		err = rows.StructScan(item)
		if err != nil {
			return nil, fmt.Errorf("cannot read history (query scan): %w", err)
		}
		rtn = append(rtn, item)
	}
	reverseHistorySlice(rtn)
	return rtn, nil
}
