package log

import (
	"fmt"
	logger "log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// DEBUG used to specify the "debug" level in order to print all the log messages
	DEBUG = 1
	// INFO used to specify the "info" level in order to print the info + error + fatal messages
	INFO = 2
	// ERROR used to specify the "error" level in order to print the error + fatal messages
	ERROR = 3
	// FATAL used to specify the "fatal" level in order to print only the fatal log lines
	FATAL = 4
)

var level = 0
var file *os.File
var path string
var maxSize int64
var mutex = new(sync.Mutex)

// Levels Different allowed debugging levels, the allowed levels are: DEBUG,
// INFO, ERROR, FATAL
var Levels = map[string]int{
	"DEBUG": DEBUG,
	"INFO":  INFO,
	"ERROR": ERROR,
	"FATAL": FATAL,
}

// SetLogger Sets the global logger level, and the path and size of the log
// file to be used as output for the logs, in case of this method is not
// called, all the logs will be print on the standar output
func SetLogger(newLevel int, filePath string, maxSizeMB int64) {
	level = newLevel
	maxSize = maxSizeMB * 1024000
	setLogFile(filePath)
}

// Debug Adds a new log line to the logs file in case of being in a DEBUG level
// or higer
func Debug(v ...interface{}) {
	if level <= DEBUG {
		_, file, line, _ := runtime.Caller(1)
		fileParts := strings.Split(file, "/")
		newLog(fmt.Sprintf("DEBUG: <%s:%d> ", fileParts[len(fileParts)-1], line), v...)
	}
}

// Info Adds a new log line to the logs file in case of being in a INFO level
// or higer
func Info(v ...interface{}) {
	if level <= INFO {
		newLog("INFO: ", v...)
	}
}

// Error Adds a new log line to the logs file in case of being in a ERROR level
// or higer
func Error(v ...interface{}) {
	if level <= ERROR {
		_, file, line, _ := runtime.Caller(1)
		fileParts := strings.Split(file, "/")
		newLog(fmt.Sprintf("ERROR: <%s:%d> ", fileParts[len(fileParts)-1], line), v...)
	}
}

// Fatal Adds a new log line to the logs file and interrupts the execution of
// the application
func Fatal(v ...interface{}) {
	if level <= FATAL {
		_, file, line, _ := runtime.Caller(1)
		fileParts := strings.Split(file, "/")
		newLog(fmt.Sprintf("FATAL: <%s:%d> ", fileParts[len(fileParts)-1], line), v...)
	}
}

// setLogFile Sets the specified path as new log file, in case of have defined
// a previous log file, rotates this
func setLogFile(filePath string) {
	if file != nil {
		file.Close()
		os.Rename(path, fmt.Sprintf("%s_%d.old", path, int32(time.Now().Unix())))
	}

	path = filePath
	if outFile, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666); err == nil {
		file = outFile
		logger.SetOutput(file)
	} else {
		Fatal("Can't open the log file:", filePath)
	}
}

// newLog Adds a new log line to the logger file with the specified level at
// the begging
func newLog(l string, v ...interface{}) {
	if file != nil {
		mutex.Lock()
		fStat, err := file.Stat()
		if err != nil {
			Fatal("Can't stat logger file")
		}
		if fStat.Size() > maxSize {
			fmt.Println("ROTATE", fStat.Size(), maxSize)
			logger.Print("Rotating log file")
			setLogFile(path)
		}
		mutex.Unlock()
	}
	logger.Print(l, fmt.Sprintln(v...))
}
