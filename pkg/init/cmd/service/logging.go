package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"syscall"

	log "github.com/sirupsen/logrus"
)

var (
	errLoggingNotEnabled = errors.New("logging system not enabled")
	logWriteSocket       = "/var/run/external-logging.sock"
	logReadSocket        = "/var/run/memlogdq.sock"
)

const (
	logDumpCommand byte = iota
)

// Log sets up a log destination for a named service.
type Log interface {
	Path(name string) string            // Path of the log file (or FIFO)
	Open(name string) (*os.File, error) // Opens a log file directly
	DumpAll()                           // Copies all the logs to the console
}

// GetLog returns the log destination we should use.
func GetLog(logDir string) Log {
	// is an external logging system enabled?
	_, err := os.Stat(logWriteSocket)
	if !os.IsNotExist(err) {
		return &remoteLog{
			fifoDir: "/var/run",
		}
	}
	return &fileLog{
		dir: logDir,
	}
}

type fileLog struct {
	dir string
}

// Path returns the name of a log file path for the named service.
func (f *fileLog) Path(name string) string {
	// We just need this to exist. If we cannot write to the directory,
	// we'll discard output instead.
	file, err := f.Open(name)
	if err != nil {
		return "/dev/null"
	}
	_ = file.Close()
	return filepath.Join(f.dir, name+".log")
}

// Open a log file for the named service.
func (f *fileLog) Open(name string) (*os.File, error) {
	return os.Create(filepath.Join(f.dir, name+".log"))
}

// DumpAll copies all the logs to the console.
func (f *fileLog) DumpAll() {
	all, err := ioutil.ReadDir(f.dir)
	if err != nil {
		fmt.Printf("Error writing %s/*.log to console: %v", f.dir, err)
		return
	}
	for _, fi := range all {
		path := filepath.Join(f.dir, fi.Name())
		if filepath.Ext(path) != ".log" {
			continue
		}
		if err := dumpFile(os.Stdout, path); err != nil {
			fmt.Printf("Error writing %s to console: %v", path, err)
		}
	}
}

type remoteLog struct {
	fifoDir string
}

// Path returns the name of a FIFO connected to the logging daemon.
func (r *remoteLog) Path(name string) string {
	path := filepath.Join(r.fifoDir, name+".log")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		return "/dev/null"
	}
	go func() {
		// In a goroutine because Open of the FIFO will block until
		// containerd opens it when the task is started.
		fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
		if err != nil {
			// Should never happen: we just created the fifo
			log.Printf("failed to open fifo %s: %s", path, err)
		}
		defer syscall.Close(fd)
		if err := sendToLogger(name, fd); err != nil {
			// Should never happen: logging is enabled
			log.Printf("failed to send fifo %s to logger: %s", path, err)
		}
	}()
	return path
}

// Open a log file for the named service.
func (r *remoteLog) Open(name string) (*os.File, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		log.Fatal("Unable to create socketpair: ", err)
	}
	logFile := os.NewFile(uintptr(fds[0]), "")

	if err := sendToLogger(name, fds[1]); err != nil {
		return nil, err
	}
	return logFile, nil
}

// DumpAll copies all the logs to the console.
func (r *remoteLog) DumpAll() {
	addr := net.UnixAddr{
		Name: logReadSocket,
		Net:  "unix",
	}
	conn, err := net.DialUnix("unix", nil, &addr)
	if err != nil {
		log.Printf("Failed to connect to logger: %s", err)
		return
	}
	defer conn.Close()
	n, err := conn.Write([]byte{logDumpCommand})
	if err != nil || n < 1 {
		log.Printf("Failed to request logs from logger: %s", err)
		return
	}

	_, err = bufio.NewReader(conn).WriteTo(os.Stdout)
	if err != nil {
		log.Printf("Failed to read logs from logger: %s", err)
	}
}

func sendToLogger(name string, fd int) error {
	var ctlSocket int
	var err error
	if ctlSocket, err = syscall.Socket(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0); err != nil {
		return err
	}

	var ctlConn net.Conn
	if ctlConn, err = net.FileConn(os.NewFile(uintptr(ctlSocket), "")); err != nil {
		return err
	}
	defer ctlConn.Close()

	ctlUnixConn, ok := ctlConn.(*net.UnixConn)
	if !ok {
		// should never happen
		log.Fatal("Internal error, invalid cast.")
	}

	raddr := net.UnixAddr{Name: logWriteSocket, Net: "unixgram"}
	oobs := syscall.UnixRights(fd)
	_, _, err = ctlUnixConn.WriteMsgUnix([]byte(name), oobs, &raddr)
	if err != nil {
		return errLoggingNotEnabled
	}
	return nil
}
