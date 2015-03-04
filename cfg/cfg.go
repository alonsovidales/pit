package cfg

// Package designed as easy to use interface for parse INI files

import (
	"fmt"
	"github.com/alonsovidales/pit/log"
	"github.com/alyu/configparser"
	"strconv"
)

var cfg *configparser.Configuration
var sections = make(map[string]*configparser.Section)

// Init Loads a INI file onto memory, first try to liad the config file from
// the etc/ directory on the current path, and if the file can't be found, try
// to load it from the /etc/ directory
// The name of the file to be used has to be the specified "appName_env.ini"
func Init(appName, env string) (err error) {
	// Trying to read the config file form the /etc directory on a first
	// instance
	if cfg, err = configparser.Read(fmt.Sprintf("etc/%s_%s.ini", appName, env)); err != nil {
		cfg, err = configparser.Read(fmt.Sprintf("/etc/%s_%s.ini", appName, env))
	}

	return
}

// GetStr Returns the value of the section, subsection as string
func GetStr(sec, subsec string) string {
	return loadSection(sec).ValueOf(subsec)
}

// GetInt Returns the value of the section, subsection as integer
func GetInt(sec, subsec string) (v int64) {
	if v, err := strconv.ParseInt(loadSection(sec).ValueOf(subsec), 10, 64); err == nil {
		return v
	}

	log.Error("Configuration parameter:", sec, subsec, "can't be parsed as integer")
	return
}

// GetFloat Returns the value of the section, subsection as float
func GetFloat(sec, subsec string) (v float64) {
	if v, err := strconv.ParseFloat(loadSection(sec).ValueOf(subsec), 64); err == nil {
		return v
	}

	log.Error("Configuration parameter:", sec, subsec, "can't be parsed as integer")
	return
}

// GetBool Returns the value of the section, subsection as boolean
func GetBool(sec, subsec string) (v bool) {
	vSec := loadSection(sec).ValueOf(subsec)

	return vSec == "1" || vSec == "true"
}

// loadSection loads a section of the config file
func loadSection(name string) (section *configparser.Section) {
	if section, ok := sections[name]; ok {
		return section
	}

	if cfg == nil {
		log.Fatal("Configuration file not yet loaded, call to the Init method before try to use the config manager")
	}

	if sec, err := cfg.Section(name); err == nil {
		sections[name] = sec

	} else {
		log.Fatal("Configuration subsection:", name, "can't be parsed")
	}

	return sections[name]
}
