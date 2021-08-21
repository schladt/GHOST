// Package client database utilies
package client

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	// blank import required by package
	_ "github.com/mattn/go-sqlite3"
)

// DBDRIVERNAME ...
const DBDRIVERNAME = "sqlite3"

// Database used by methods
type Database struct {
	Db   *sql.DB
	Name string
}

// Init method to initialize database
func (db *Database) Init() error {
	var err error
	if db.Name == "" {
		return errors.New("Database Name cannot be empty")
	}
	db.Db, err = sql.Open(DBDRIVERNAME, db.Name)
	return err
}

// Vacuum method to execute the VACUUM command
func (db *Database) Vacuum() error {
	//build and execute query
	stmtStr := `VACUUM;`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec()

	return err
}

// KeyStoreCreateTable method to create key_table table if not exist
func (db *Database) KeyStoreCreateTable() error {
	stmtStr := `CREATE TABLE 
				IF NOT EXISTS key_store(
					key TEXT UNIQUE,
					data TEXT,
					rowid INTEGER PRIMARY KEY ASC);`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec()
	return err
}

// KeyStoreInsert inserts data into key_store
func (db *Database) KeyStoreInsert(key string, data string) error {
	//create table if needed
	err := db.KeyStoreCreateTable()
	if err != nil {
		return err
	}

	//build and execute update statement
	stmtStr := `UPDATE key_store 
				SET data=? 
				WHERE key=?;`
	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(data, key)

	//build and execute insert statement
	stmtStr = `INSERT OR IGNORE INTO key_store( 
					key,
					data) 
				VALUES (?, ?);`
	stmt, err = db.Db.Prepare(stmtStr)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(key, data)

	return err
}

// KeyStoreDelete removes a key pair from the key_store table
// Returns true if a key pair was actually removed
func (db *Database) KeyStoreDelete(key string) (bool, error) {
	//create table if needed
	err := db.KeyStoreCreateTable()
	if err != nil {
		return false, err
	}

	//build and execute query
	stmtStr := `DELETE FROM key_store 
				WHERE key=?;`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return false, err
	}
	defer stmt.Close()

	result, err := stmt.Exec(key)
	n, err := result.RowsAffected()

	return (n == int64(1)), err
}

// KeyStoreDeleteAll removes all key pairs with keys matching the subkey
// Returns number of row removed as int64
func (db *Database) KeyStoreDeleteAll(subkey string) (int64, error) {
	//create table if needed
	err := db.KeyStoreCreateTable()
	if err != nil {
		return 0, err
	}

	//build and execute query
	stmtStr := `DELETE FROM key_store 
				WHERE key LIKE %?%;`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	result, err := stmt.Exec(subkey)
	return result.RowsAffected()
}

// KeyStoreSelect Returns stored value for a stored key pair
// Used for core data only -- plugin data stored in plugin_store table
func (db *Database) KeyStoreSelect(key string) (string, error) {
	//initialize return string
	var outStr string

	//create table if needed
	err := db.KeyStoreCreateTable()
	if err != nil {
		return outStr, err
	}

	//build and execute query
	stmtStr := `SELECT data
				FROM key_store 
				WHERE key=?;`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return outStr, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(key)
	if err != nil {
		return outStr, err
	}
	defer rows.Close()

	//parse results
	if rows.Next() {
		err = rows.Scan(&outStr)
		if err != nil {
			return outStr, err
		}
	}

	return outStr, err
}

// KeyStoreGetSubkeys returns a list of subkeys for a given prefix
func (db *Database) KeyStoreGetSubkeys(prefix string) ([]string, error) {
	//initialize return string list
	var subkeys []string

	//create table if needed
	err := db.KeyStoreCreateTable()
	if err != nil {
		return subkeys, err
	}

	//build and execute query
	stmtStr := `SELECT key
				FROM key_store 
				WHERE key LIKE ?;`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return subkeys, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(prefix + "%")
	if err != nil {
		return subkeys, err
	}
	defer rows.Close()

	//parse results
	for rows.Next() {
		var key string
		err = rows.Scan(&key)
		if err != nil {
			return subkeys, err
		}
		subkeys = append(subkeys, key)
	}

	return subkeys, err

}

// PluginCreateTable method to create key_table table if not exist
func (db *Database) PluginCreateTable() error {
	stmtStr := `CREATE TABLE 
				IF NOT EXISTS plugins(
					uuid TEXT UNIQUE,
					name TEXT, 
					mode TEXT,
					process_name TEXT,
					process_id INTEGER,
					current_manager INTEGER,
					status TEXT,
					status_message TEXT,
					last_exit TEXT,
					last_start TEXT,
					rowid INTEGER PRIMARY KEY ASC);`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec()
	return err
}

// PluginInsert stores plugin values to Database
func (db *Database) PluginInsert(p Plugin) error {
	// create table if needed
	err := db.PluginCreateTable()
	if err != nil {
		return err
	}

	// build and execute update statement
	stmtStr := `UPDATE plugins 
		SET name=?, mode=?, process_name=?, status=?, status_message=?, last_exit=?, last_start=?, process_id=?, current_manager=? 
		WHERE uuid=?;`
	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(p.Name, p.Mode, p.ProcessName, p.Status, p.StatusMessage, p.LastExit.Format(time.RFC3339Nano), p.LastStart.Format(time.RFC3339Nano), p.ProcessID, p.CurrentManager, p.UUID)

	// build and execute insert statement
	stmtStr = `INSERT OR IGNORE INTO plugins( 
			uuid, name, mode, process_name, status, status_message, last_exit, last_start, process_id, current_manager) 
		VALUES (?,?,?,?,?,?,?,?,?,?);`
	stmt, err = db.Db.Prepare(stmtStr)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(p.UUID, p.Name, p.Mode, p.ProcessName, p.Status, p.StatusMessage, p.LastExit.Format(time.RFC3339Nano), p.LastStart.Format(time.RFC3339Nano), p.ProcessID, p.CurrentManager)

	return err
}

// PluginSelectUUID returns plugin struct from DB given a uuid
// INPUT: uuid (string)
// OUTPUT: Plugin struct. If no plugin is found, the plugin uuid member will be an empty string.
func (db *Database) PluginSelectUUID(uuid string) (p Plugin, err error) {

	//create table if needed
	if err := db.PluginCreateTable(); err != nil {
		return p, err
	}

	//build and execute query
	stmtStr := `SELECT 
					uuid,
					name, 
					mode,
					process_name,
					process_id,
					status,
					status_message,
					last_exit,
					last_start,
					current_manager
				FROM plugins 
				WHERE uuid=?;`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return p, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(uuid)
	if err != nil {
		return p, err
	}
	defer rows.Close()

	// containers to hold time strings before parsing
	var lastExit string
	var lastStart string

	//parse results
	if rows.Next() {
		err = rows.Scan(&p.UUID, &p.Name, &p.Mode, &p.ProcessName, &p.ProcessID, &p.Status, &p.StatusMessage, &lastExit, &lastStart, &p.CurrentManager)
	}
	if err != nil || p.UUID == "" {
		return p, err
	}

	// parse time strings
	p.LastExit, err = time.Parse(time.RFC3339Nano, lastExit)
	p.LastStart, err = time.Parse(time.RFC3339Nano, lastStart)

	return p, err
}

// PluginSelectMode returns list of plugins given a mode
// INPUT: mode (string)
// Output: list of Plugins
func (db *Database) PluginSelectMode(mode string) (plugins []Plugin, err error) {

	//create table if needed
	if err := db.PluginCreateTable(); err != nil {
		return plugins, err
	}

	//build and execute query
	stmtStr := `SELECT 
						uuid,
						name, 
						mode,
						process_name,
						process_id,
						status,
						status_message,
						last_exit,
						last_start
					FROM plugins 
					WHERE mode LIKE ?;`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return plugins, err
	}
	defer stmt.Close()

	rows, err := stmt.Query("%" + mode + "%")
	if err != nil {
		return plugins, err
	}
	defer rows.Close()

	// parse results
	for rows.Next() {
		// containers to hold results
		p := Plugin{}
		var lastExit string
		var lastStart string

		// parse the row
		err = rows.Scan(&p.UUID, &p.Name, &p.Mode, &p.ProcessName, &p.ProcessID, &p.Status, &p.StatusMessage, &lastExit, &lastStart)
		if err != nil {
			return plugins, err
		}

		// add to list if plugin actually returned
		if p.UUID != "" {
			// parse time strings
			p.LastExit, err = time.Parse(time.RFC3339Nano, lastExit)
			p.LastStart, err = time.Parse(time.RFC3339Nano, lastStart)
			plugins = append(plugins, p)
		}
	}
	return plugins, err
}

// PluginSelectStatus returns list of plugins given a status
// INPUT: status (string)
// Output: list of Plugins
func (db *Database) PluginSelectStatus(status string) (plugins []Plugin, err error) {

	//create table if needed
	if err := db.PluginCreateTable(); err != nil {
		return plugins, err
	}

	//build and execute query
	stmtStr := `SELECT 
						uuid,
						name, 
						mode,
						process_name,
						process_id,
						status,
						status_message,
						last_exit,
						last_start
					FROM plugins 
					WHERE status LIKE ?;`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return plugins, err
	}
	defer stmt.Close()

	rows, err := stmt.Query("%" + status + "%")
	if err != nil {
		return plugins, err
	}
	defer rows.Close()

	//parse results
	for rows.Next() {
		// containers to hold results
		p := Plugin{}
		var lastExit string
		var lastStart string

		// parse the row
		err = rows.Scan(&p.UUID, &p.Name, &p.Mode, &p.ProcessName, &p.ProcessID, &p.Status, &p.StatusMessage, &lastExit, &lastStart)
		if err != nil {
			return plugins, err
		}

		// add to list if plugin actually returned
		if p.UUID != "" {
			// parse time strings
			p.LastExit, err = time.Parse(time.RFC3339Nano, lastExit)
			p.LastStart, err = time.Parse(time.RFC3339Nano, lastStart)
			plugins = append(plugins, p)
		}
	}
	return plugins, err
}

// MessageQueueCreateTable method to create message_queue table if not exist
func (db *Database) MessageQueueCreateTable() error {
	stmtStr := `CREATE TABLE IF NOT EXISTS message_queue( 
				post_string TEXT, 
				post_uri TEXT,
				rowid INTEGER PRIMARY KEY ASC);`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec()
	if err != nil {
		return err
	}

	triggerStr := `
		CREATE TRIGGER IF NOT EXISTS rolling_queue AFTER INSERT ON message_queue
		   BEGIN
		     DELETE FROM message_queue WHERE rowid <= (SELECT rowid FROM message_queue ORDER BY rowid DESC LIMIT 20000, 1);
		   END;`

	triggerStmt, err := db.Db.Prepare(triggerStr)
	//defer triggerStmt.Close()
	if err != nil {
		return err
	}
	_, err = triggerStmt.Exec()

	return err
}

// MessageQueueSelectURI returns a string map list with the first 100 messages in the queue
// Responses are limited to 100 results
// INPUT uri (string) - post uris to filter search on
// OUTPUT outMsgs ([]string) - list of output messages
// OUPUT rowIds ([]int) - list of ints corresponding to row ids that should be removed from the database once messages are transmitted
func (db *Database) MessageQueueSelectURI(uri string) (outMsgs []string, rowIds []int, err error) {

	//create table if needed
	err = db.MessageQueueCreateTable()
	if err != nil {
		return
	}

	//build and execute query
	stmtStr := `SELECT  
					rowid,
					post_string  
				FROM message_queue 
				ORDER BY ROWID 
				LIMIT 100;`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		return
	}
	defer rows.Close()

	//parse results
	for rows.Next() {
		var rowid int
		var postString string
		err = rows.Scan(&rowid, &postString)
		if err != nil {
			return
		}

		// add to list results actually returned
		if postString != "" {
			outMsgs = append(outMsgs, postString)
		}
		if rowid != 0 {
			rowIds = append(rowIds, rowid)
		}
	}
	return
}

// MessageQueueInsert inserts messages into the message_queue table
func (db *Database) MessageQueueInsert(postString string, postURI string) error {
	//create table if needed
	err := db.MessageQueueCreateTable()
	if err != nil {
		return err
	}

	//build and execute query
	stmtStr := `INSERT INTO message_queue(  
					post_string, 
					post_uri) 
				VALUES(?, ?);`

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(postString, postURI)

	return err
}

// MessageQueueDelete deletes messages by rowID from the message_queue table
// INPUT rowIds []int - list of rowids to remove
// Returns number of rows deleted
func (db *Database) MessageQueueDelete(rowIds []int) (int, error) {
	//create table if needed
	err := db.MessageQueueCreateTable()
	if err != nil {
		return 0, err
	}

	//build and execute query
	stmtStr := "DELETE FROM message_queue WHERE rowid IN (?" + strings.Repeat(",?", len(rowIds)-1) + ")"

	stmt, err := db.Db.Prepare(stmtStr)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	// turn list of ints into list of interfaces
	args := make([]interface{}, len(rowIds))
	for i := range rowIds {
		args[i] = rowIds[i]
	}

	// execute sql
	result, err := stmt.Exec(args...)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()

	return int(n), err
}
