package utils

import (
	"log"
	"os"
)

var (
	// Verbose controls whether [INFO] and emoji-heavy logs are printed
	Verbose bool
)

func init() {
	// Default to false unless HDN_VERBOSE=true
	Verbose = os.Getenv("HDN_VERBOSE") == "true"
}

// LogPrintf prints formatted logs only if Verbose is true
func LogPrintf(format string, v ...interface{}) {
	if Verbose {
		log.Printf(format, v...)
	}
}

// LogPrintln prints logs only if Verbose is true
func LogPrintln(v ...interface{}) {
	if Verbose {
		log.Println(v...)
	}
}

// ForceLogPrintf always prints the log, regardless of Verbose setting (for errors/critical info)
func ForceLogPrintf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

// QuietLogPrintf handles "nice" logs that user wants to reduce
func QuietLogPrintf(prefix string, format string, v ...interface{}) {
	if Verbose {
		log.Printf("[%s] "+format, append([]interface{}{prefix}, v...)...)
	}
}
