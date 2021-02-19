package progressbar

import (
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jfrog/jfrog-cli-core/utils/coreutils"
	corelog "github.com/jfrog/jfrog-cli-core/utils/log"
	logUtils "github.com/jfrog/jfrog-cli/utils/log"
	"github.com/jfrog/jfrog-client-go/utils"
	ioUtils "github.com/jfrog/jfrog-client-go/utils/io"
	"github.com/jfrog/jfrog-client-go/utils/log"
	"github.com/vbauerster/mpb/v4"
	"github.com/vbauerster/mpb/v4/decor"
	"golang.org/x/crypto/ssh/terminal"
)

var terminalWidth int

const progressBarWidth = 20
const minTerminalWidth = 70
const progressRefreshRate = 200 * time.Millisecond

type progressBarManager struct {
	// A list of progress bar objects.
	bars []progressBar
	// A wait group for all progress bars.
	barsWg *sync.WaitGroup
	// A container of all external mpb bar objects to be displayed.
	container *mpb.Progress
	// A synchronization lock object.
	barsRWMutex sync.RWMutex
	// A general work indicator spinner.
	headlineBar *mpb.Bar
	// A bar that displies the path of the log file.
	logFilePathBar *mpb.Bar
	// A general tasks completion indicator.
	generalProgressBar *mpb.Bar
	// A cumulative amount of tasks
	tasksCount int64
}

type progressBarUnit struct {
	bar         *mpb.Bar
	incrChannel chan int
	description string
}

type progressBar interface {
	ioUtils.Progress
	getProgressBarUnit() *progressBarUnit
}

// Initializes a new reader progress indicator for a new file transfer.
// Input: 'total' - file size.
//		  'prefix' - optional description.
//		  'extraInformation' - extra information for disply.
// Output: progress indicator id
func (p *progressBarManager) NewProgressReader(total int64, prefix, extraInformation string) (bar ioUtils.Progress) {
	// Write Lock when appending a new bar to the slice
	p.barsRWMutex.Lock()
	defer p.barsRWMutex.Unlock()
	p.barsWg.Add(1)
	newBar := p.container.AddBar(int64(total),
		mpb.BarStyle("|🟩🟩⬛|"),
		mpb.BarRemoveOnComplete(),
		mpb.AppendDecorators(
			// Extra chars length is the max length of the KibiByteCounter
			decor.Name(buildProgressDescription(prefix, extraInformation, 17)),
			decor.CountersKibiByte("%3.1f/%3.1f"),
		),
	)

	// Add bar to bars array
	unit := initNewBarUnit(newBar, extraInformation)
	barId := len(p.bars) + 1
	readerProgressBar := ReaderProgressBar{progressBarUnit: unit, Id: barId}
	p.bars = append(p.bars, &readerProgressBar)
	return &readerProgressBar
}

// Changes progress indicator state and acts accordingly.
func (p *progressBarManager) SetProgressState(id int, state string) {
	switch state {
	case "Merging":
		p.addNewMergingSpinner(id)
	}
}

// Initializes a new progress bar, that replaces the progress bar with the given replacedBarId
func (p *progressBarManager) addNewMergingSpinner(replacedBarId int) {
	// Write Lock when appending a new bar to the slice
	p.barsRWMutex.Lock()
	defer p.barsRWMutex.Unlock()
	replacedBar := p.bars[replacedBarId-1].getProgressBarUnit()
	p.bars[replacedBarId-1].Abort()
	newBar := p.container.AddSpinner(1, mpb.SpinnerOnMiddle,
		mpb.SpinnerStyle(createSpinnerFramesArray()),
		mpb.AppendDecorators(
			decor.Name(buildProgressDescription("  Merging  ", replacedBar.description, 0)),
		),
	)
	// Bar replacement is a simple spinner and thus does not implement any read functionality
	unit := &progressBarUnit{bar: newBar, description: replacedBar.description}
	progressBar := SimpleProgressBar{progressBarUnit: unit, Id: replacedBarId}
	p.bars[replacedBarId-1] = &progressBar
}

func buildProgressDescription(prefix, path string, extraCharsLen int) string {
	separator := " | "
	// Max line length after decreasing bar width (*2 in case unicode chars with double width are used) and the extra chars
	descMaxLength := terminalWidth - (progressBarWidth*2 + extraCharsLen)
	return buildDescByLimits(descMaxLength, " "+prefix+separator, path, separator)
}

func buildDescByLimits(descMaxLength int, prefix, path, suffix string) string {
	desc := prefix + path + suffix

	// Verify that the whole description doesn't exceed the max length
	if len(desc) <= descMaxLength {
		return desc
	}

	// If it does exceed, check if shortening the path will help (+3 is for "...")
	if len(desc)-len(path)+3 > descMaxLength {
		// Still exceeds, do not display desc
		return ""
	}

	// Shorten path from the beginning
	path = "..." + path[len(desc)-descMaxLength+3:]
	return prefix + path + suffix
}

func initNewBarUnit(bar *mpb.Bar, extraInformation string) *progressBarUnit {
	ch := make(chan int, 1000)
	unit := &progressBarUnit{bar: bar, incrChannel: ch, description: extraInformation}
	go incrBarFromChannel(unit)
	return unit
}

func incrBarFromChannel(unit *progressBarUnit) {
	// Increase bar while channel is open
	for n := range unit.incrChannel {
		unit.bar.IncrBy(n)
	}
}

func createSpinnerFramesArray() []string {
	black := "⬛"
	green := "🟩"
	spinnerFramesArray := make([]string, progressBarWidth)
	for i := 1; i < progressBarWidth-1; i++ {
		cur := "|" + strings.Repeat(black, i-1) + green + strings.Repeat(black, progressBarWidth-2-i) + "|"
		spinnerFramesArray[i] = cur
	}
	return spinnerFramesArray
}

// Aborts a progress bar.
// Should be called even if bar completed successfully.
// The progress component's Abort method has no effect if bar has already completed, so can always be safely called anyway
func (p *progressBarManager) RemoveProgress(id int) {
	p.barsRWMutex.RLock()
	defer p.barsWg.Done()
	defer p.barsRWMutex.RUnlock()
	p.bars[id-1].Abort()
	p.generalProgressBar.Increment()

}

// Quits the progress bar while aborting the initial bars.
func (p *progressBarManager) Quit() {
	if p.headlineBar != nil {
		p.headlineBar.Abort(true)
		p.barsWg.Done()
	}
	if p.logFilePathBar != nil {
		p.barsWg.Done()
	}
	if p.generalProgressBar != nil {
		p.generalProgressBar.Abort(true)
		p.barsWg.Done()
	}
	// Wait a refresh rate to make sure all aborts have finished
	time.Sleep(progressRefreshRate)
	p.container.Wait()
}

func (p *progressBarManager) GetProgress(id int) ioUtils.Progress {
	return p.bars[id-1]
}

// Initializes progress bar if possible (all conditions in 'shouldInitProgressBar' are met).
// Creates a log file and sets the Logger to it. Caller responsible to close the file.
// Returns nil, nil, err if failed.
func InitProgressBarIfPossible() (ioUtils.ProgressMgr, *os.File, error) {
	shouldInit, err := shouldInitProgressBar()
	if !shouldInit || err != nil {
		return nil, nil, err
	}

	logFile, err := logUtils.CreateLogFile()
	if err != nil {
		return nil, nil, err
	}
	log.SetLogger(log.NewLogger(corelog.GetCliLogLevel(), logFile))

	newProgressBar := &progressBarManager{}
	newProgressBar.barsWg = new(sync.WaitGroup)

	// Initialize the progressBar container with wg, to create a single joint point
	newProgressBar.container = mpb.New(
		mpb.WithOutput(os.Stderr),
		mpb.WithWaitGroup(newProgressBar.barsWg),
		mpb.WithWidth(progressBarWidth),
		mpb.WithRefreshRate(progressRefreshRate))

	// Add headline bar to the whole progress
	newProgressBar.printLogFilePathAsBar(logFile.Name())
	newProgressBar.newHeadlineBar(" Working... ")
	newProgressBar.tasksCount = 0
	newProgressBar.newGeneralProgressBar()

	return newProgressBar, logFile, nil
}

// Init progress bar if all required conditions are met:
// CI == false (or unset), Stderr is a terminal, and terminal width is large enough
func shouldInitProgressBar() (bool, error) {
	ci, err := utils.GetBoolEnvValue(coreutils.CI, false)
	if ci || err != nil {
		return false, err
	}
	if !isTerminal() {
		return false, err
	}
	err = setTerminalWidthVar()
	if err != nil {
		return false, err
	}
	return terminalWidth >= minTerminalWidth, nil
}

// Check if Stderr is a terminal
func isTerminal() bool {
	return terminal.IsTerminal(int(os.Stderr.Fd()))
}

// Get terminal dimensions
func setTerminalWidthVar() error {
	width, _, err := terminal.GetSize(int(os.Stderr.Fd()))
	// -5 to avoid edges
	terminalWidth = width - 5
	return err
}

// Initializes a new progress bar for general progress indication
func (p *progressBarManager) newGeneralProgressBar() {
	p.barsWg.Add(1)
	p.generalProgressBar = p.container.AddBar(
		p.tasksCount,
		mpb.BarStyle("|⬜⬜⬛|"),
		mpb.BarRemoveOnComplete(),
		mpb.AppendDecorators(
			decor.Name(" Tasks: "),
			decor.CountersNoUnit("%d/%d"),
		),
	)
}

// Initializes a new progress bar for headline, with a spinner
func (p *progressBarManager) newHeadlineBar(headline string) {
	p.barsWg.Add(1)
	p.headlineBar = p.container.AddSpinner(1, mpb.SpinnerOnLeft,
		mpb.SpinnerStyle([]string{"-", "-", "\\", "\\", "|", "|", "/", "/"}),
		mpb.BarRemoveOnComplete(),
		mpb.PrependDecorators(
			decor.Name(headline),
		),
	)
}

// Initializes a new progress bar that states the log file path. The bar's text remains after cli is done.
func (p *progressBarManager) printLogFilePathAsBar(path string) {
	p.barsWg.Add(1)
	prefix := " Log path: "
	p.logFilePathBar = p.container.AddBar(0,
		mpb.BarClearOnComplete(),
		mpb.PrependDecorators(
			decor.Name(buildDescByLimits(terminalWidth, prefix, path, "")),
		),
	)
	p.logFilePathBar.SetTotal(0, true)
}

// IncGeneralProgressTotalBy incremenates the general progress bar total count by given n.
func (p *progressBarManager) IncGeneralProgressTotalBy(n int64) {
	atomic.AddInt64(&p.tasksCount, n)
	if p.generalProgressBar != nil {
		p.generalProgressBar.SetTotal(p.tasksCount, false)
	}
}
