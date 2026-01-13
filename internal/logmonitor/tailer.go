package logmonitor

import (
	"bufio"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LineHandler is called when a new line is read from a log file
type LineHandler func(source string, line string)

// Tailer tails a single log file, handling rotation
type Tailer struct {
	path    string
	handler LineHandler

	file     *os.File
	reader   *bufio.Reader
	position int64
	inode    uint64

	stopCh chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
}

// NewTailer creates a new tailer for a log file
func NewTailer(path string, handler LineHandler) *Tailer {
	return &Tailer{
		path:    path,
		handler: handler,
		stopCh:  make(chan struct{}),
	}
}

// Start begins tailing the file
func (t *Tailer) Start() error {
	if err := t.openFile(); err != nil {
		// File might not exist yet - that's OK, we'll poll for it
		log.Printf("Log file not found (will poll): %s", t.path)
	}

	t.wg.Add(1)
	go t.tailLoop()

	return nil
}

// Stop stops tailing
func (t *Tailer) Stop() {
	close(t.stopCh)
	t.wg.Wait()

	t.mu.Lock()
	if t.file != nil {
		t.file.Close()
	}
	t.mu.Unlock()
}

// openFile opens the log file and seeks to the end
func (t *Tailer) openFile() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	file, err := os.Open(t.path)
	if err != nil {
		return err
	}

	// Get file info for inode tracking (rotation detection)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}

	// Seek to end - we only want new lines
	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		file.Close()
		return err
	}

	t.file = file
	t.reader = bufio.NewReader(file)
	t.position = offset
	t.inode = getInode(info)

	log.Printf("Tailing log file: %s (position: %d)", t.path, offset)

	return nil
}

// tailLoop continuously reads new lines from the file
func (t *Tailer) tailLoop() {
	defer t.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	rotationCheckTicker := time.NewTicker(5 * time.Second)
	defer rotationCheckTicker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-rotationCheckTicker.C:
			t.checkRotation()
		case <-ticker.C:
			t.readLines()
		}
	}
}

// readLines reads any available lines from the file
func (t *Tailer) readLines() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.file == nil {
		// Try to open the file if it doesn't exist
		if err := t.openFileUnlocked(); err != nil {
			return
		}
	}

	for {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading log file %s: %v", t.path, err)
			}
			break
		}

		// Update position
		t.position += int64(len(line))

		// Remove trailing newline
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}

		// Skip empty lines
		if len(line) == 0 {
			continue
		}

		// Get relative source path
		source := filepath.Base(t.path)

		// Call handler
		if t.handler != nil {
			t.handler(source, line)
		}
	}
}

// checkRotation checks if the file has been rotated
func (t *Tailer) checkRotation() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.file == nil {
		// Try to open if not open
		t.openFileUnlocked()
		return
	}

	// Stat the current file path
	info, err := os.Stat(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			// File was deleted (rotated away)
			log.Printf("Log file rotated (deleted): %s", t.path)
			t.file.Close()
			t.file = nil
			t.reader = nil
			return
		}
		return
	}

	// Check if inode changed (file was replaced)
	newInode := getInode(info)
	if newInode != t.inode {
		log.Printf("Log file rotated (inode changed): %s", t.path)
		t.file.Close()
		t.file = nil
		t.reader = nil

		// Try to open the new file
		t.openFileUnlocked()
		return
	}

	// Check if file was truncated
	if info.Size() < t.position {
		log.Printf("Log file truncated: %s (was %d, now %d)", t.path, t.position, info.Size())
		// Seek back to start
		t.file.Seek(0, io.SeekStart)
		t.reader = bufio.NewReader(t.file)
		t.position = 0
	}
}

// openFileUnlocked opens the file without locking (caller must hold lock)
func (t *Tailer) openFileUnlocked() error {
	file, err := os.Open(t.path)
	if err != nil {
		return err
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}

	// Seek to end
	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		file.Close()
		return err
	}

	t.file = file
	t.reader = bufio.NewReader(file)
	t.position = offset
	t.inode = getInode(info)

	log.Printf("Opened log file: %s (position: %d)", t.path, offset)

	return nil
}

// getInode gets the inode of a file (for rotation detection)
// This is platform-specific
func getInode(info os.FileInfo) uint64 {
	// Use the file modification time as a fallback "inode" on platforms
	// where we can't get the real inode
	return uint64(info.ModTime().UnixNano())
}
