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

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/scripthaus-dev/scripthaus/pkg/base"
)

const VersionMdKey = "version"
const RunTypePlaybook = "playbook"
const RunTypeScript = "script"

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

type DBMeta struct {
	Version int
}

type HistoryMetaType struct{}

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
	DurationMs int64 // update
	ExitCode   int   // update
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
	db, err := GetDBConn()
	if err != nil {
		return nil
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

func InsertHistoryItem(item *HistoryItem) error {
	sqlStr := `
        INSERT INTO history 
            (historyid, ts, scversion, runtype, scriptpath, scriptfile, scriptname, scripttype, metadata, cwd, hostname, ipaddr, sysuser, cmdline)
        VALUES 
            (NULL, :ts, :scversion, :runtype, :scriptpath, :scriptfile, :scriptname, :scripttype, :metadata, :cwd, :hostname, :ipaddr, :sysuser, :cmdline)
`
	db, err := GetDBConn()
	if err != nil {
		return nil
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
	db, err := GetDBConn()
	if err != nil {
		return err
	}
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
	if len(os.Args) >= 2 {
		rtn.CmdLine = marshalJsonNoErr(os.Args[1:])
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

func GetScHomeDir() (string, error) {
	scHome := os.Getenv(base.ScHomeVarName)
	if scHome == "" {
		homeVar := os.Getenv(base.HomeVarName)
		if homeVar == "" {
			return "", fmt.Errorf("Cannot resolve scripthaus home directory (SCRIPTHAUS_HOME and HOME not set)")
		}
		scHome = path.Join(homeVar, "scripthaus")
	}
	return scHome, nil
}

func GetHistoryDBFileName() (string, error) {
	scHome, err := GetScHomeDir()
	if err != nil {
		return "", err
	}
	return path.Join(scHome, base.DBFileName), nil
}

func createDB() error {
	scHomeDir, err := GetScHomeDir()
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
		return fmt.Errorf("invalid scripthaus home directory '%s' is a file (not a directory)", scHomeDir, err)
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

func GetDBConn() (*sqlx.DB, error) {
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
