package rlog

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	EMEGR = iota
	ALERT
	CRIT
	ERROR
	WARN
	NOTICE
	INFO
	DEBUG
	TRACE
)

const (
	FACILITY_SYS   = 0 << 3
	FACILITY_ADMIN = 1 << 3
	FACILITY_SEC   = 13 << 3
	FACILITY_APP   = 16 << 3
	FACILITY_DB    = 17 << 3
	FACILITY_GAME  = 18 << 3
	FACILITY_USER  = 19 << 3
)

type Outputer int

const (
	STD = iota
	FILE
)

const (
	CONSOLE = 1 << iota
	LOCAL
	REMOTE
)

type logger struct {
	logFd         *os.File
	starLev       int      
	buf           []byte   
	path          string   
	baseName      string   
	logName       string  
	debugOutputer Outputer 
	debugSwitch   bool     
	callDepth     int      
	fullPath      string   
	lastHour      int     
	lastDate      string  
	IsShowConsole bool     
	logChan       chan string
	saveFlags     int // Decide if save or send to remote
	remoteLog     *remote
	msgQueue      []string
	msgQueueMutex sync.Mutex
}

var gLogger *logger
var DBLogger  = &gLogger
var once sync.Once

func InitLog(level int, saveFlag int) {
	gLogger = newLogger("./logs", "", "Log4Golang", level, saveFlag)
	gLogger.setCallDepth(3)
	gLogger.start()

}

func EnableRemoteLog(ident string, addr string) {
	if nil == gLogger {
		panic("init log first")
	} else {
		gLogger.saveFlags |= REMOTE
		if gLogger.remoteLog != nil {
			gLogger.remoteLog.start(ident, addr)
		} else {
			gLogger.remoteLog = newRemote()
			gLogger.remoteLog.start(ident, addr)
		}
	}
}

func newLogger(path, baseName, logName string, level int, saveFlags int) *logger {
	logger := &logger{path: path, baseName: baseName, logName: logName, starLev: level}
	logger.debugSwitch = true
	logger.debugOutputer = STD
	logger.callDepth = 3
	logger.logChan = make(chan string, 8096)
	logger.saveFlags = CONSOLE | saveFlags
	logger.msgQueue = make([]string, 0)
	if logger.saveFlags&REMOTE != 0 {
		gLogger.remoteLog = newRemote()
	}
	return logger
}

func (this* logger) getCurDate() string{
	now := time.Now()
	str := now.Format("2006-01-02")
	return str
}

func (this* logger) pathExists(path string)bool{
	_, err := os.Stat(path)
	if err == nil{
		return true
	}
	return false
}

func (this *logger) getLoggerFd() *os.File {
	curDate := this.getCurDate()
	if this.lastDate != curDate{
		path := strings.TrimSuffix(this.path, "/")
		path = path + "/" + curDate + "/"
		if !this.pathExists(path){
			err := os.Mkdir(path, os.ModePerm)
			if err != nil{
				fmt.Println(err)
				panic(err)
			}
		}
		this.lastDate = curDate
	}

	var err error
	path := strings.TrimSuffix(this.path, "/")
	flag := os.O_WRONLY | os.O_APPEND | os.O_CREATE
	this.fullPath = path + "/" + this.lastDate + "/"+ this.baseName
	now := time.Now()
	this.fullPath += fmt.Sprintf("%04d%02d%02d%02d.log", now.Year(), now.Month(), now.Day(), now.Hour())
	this.logFd, err = os.OpenFile(this.fullPath, flag, 0666)
	if err != nil {
		panic(err)
	}
	return this.logFd
}

func (this *logger) start() {
	if this.saveFlags&LOCAL != 0 {
		err := os.MkdirAll(this.path, os.ModePerm)
		if err != nil {
			panic(err)
		}
		this.logFd = this.getLoggerFd()
		RunCoroutine(func() {
			this.autoWrite()
		})
	}
}

func (this *logger) writeLog(buf []byte) {

	now := time.Now()
	if now.Hour() != this.lastHour {
		err := this.logFd.Close()
		if err != nil {
			str := fmt.Sprintf("close file[%v] failed[err:%v]", this.fullPath, err.Error())
			fmt.Println(str)
		}
		this.logFd = this.getLoggerFd()
		this.lastHour = now.Hour()
	}
	_, err := this.logFd.Write(buf)
	if err != nil {
		fmt.Printf("write failed, %v", err)
	}
}

func (this *logger) autoWrite() {
	for {
		d := this.pop_front()
		if d != "" {
			this.writeLog(bytes.NewBufferString(d).Bytes())
		} else {
			time.Sleep(time.Millisecond * 20)
		}
	}
}

func (this *logger) output(fd io.Writer, level, prefix string, format string, v ...interface{}) (err error) {
	var msg string
	if format == "" {
		msg = fmt.Sprintln(v...)
	} else {
		msg = fmt.Sprintf(format, v...)
	}

	this.buf = this.buf[:0]

	this.buf = append(this.buf, "["+this.logName+"]"...)
	this.buf = append(this.buf, level...)
	this.buf = append(this.buf, prefix...)

	this.buf = append(this.buf, ":"+msg...)
	if len(msg) > 0 && msg[len(msg)-1] != '\n' {
		this.buf = append(this.buf, '\n')
	}

	_, err = fd.Write(this.buf)

	return nil
}

func (l *logger) setCallDepth(d int) {
	l.callDepth = d
}

func (l *logger) openDebug() {
	l.debugSwitch = true
}

func (l *logger) getFileLine() string {
	_, file, line, ok := runtime.Caller(l.callDepth)
	if !ok {
		file = "???"
		line = 0
	}
	return l.getFileName(file) + ":" + itoa(line, -1)
}

func (l *logger) getFileName(path string) string {
	strArr := strings.Split(path, "/")
	nLen := len(strArr)
	if nLen > 0 {
		return strArr[nLen-1]
	}
	return path
}

func itoa(i int, wid int) string {
	var u uint = uint(i)
	if u == 0 && wid <= 1 {
		return "0"
	}

	// Assemble decimal in reverse order.
	var b [32]byte
	bp := len(b)
	for ; u > 0 || wid > 0; u /= 10 {
		bp--
		wid--
		b[bp] = byte(u%10) + '0'
	}
	return string(b[bp:])
}

func (l *logger) getTime() string {
	// Time is yyyy-mm-dd hh:mm:ss.microsec
	var buf []byte
	t := time.Now()
	year, month, day := t.Date()
	buf = append(buf, itoa(int(year), 4)+"-"...)
	buf = append(buf, itoa(int(month), 2)+"-"...)
	buf = append(buf, itoa(int(day), 2)+" "...)

	hour, min, sec := t.Clock()
	buf = append(buf, itoa(hour, 2)+":"...)
	buf = append(buf, itoa(min, 2)+":"...)
	buf = append(buf, itoa(sec, 2)...)

	buf = append(buf, '.')
	buf = append(buf, itoa(t.Nanosecond()/1e3, 6)...)

	return string(buf[:])
}

func (l *logger) closeDebug() {
	l.debugSwitch = false
}

func (l *logger) setDebugOutput(o Outputer) {
	l.debugOutputer = o
}

func LogTrace(facilitity int, format string, v ...interface{}) error {
	return gLogger.addlog(TRACE, facilitity, format, v...)
}

func LogDebug(facilitity int, format string, v ...interface{}) error {
	return gLogger.addlog(DEBUG, facilitity, format, v...)
}

func LogInfo(facilitity int, format string, v ...interface{}) error {
	return gLogger.addlog(INFO, facilitity, format, v...)
}

func LogWarn(facilitity int, format string, v ...interface{}) error {
	return gLogger.addlog(WARN, facilitity, format, v...)
}

func LogNotice(facilitity int, format string, v ...interface{}) error {
	return gLogger.addlog(NOTICE, facilitity, format, v...)
}

func LogError(facilitity int, format string, v ...interface{}) error {
	return gLogger.addlog(ERROR, facilitity, format, v...)
}

func LogCrit(facilitity int, format string, v ...interface{}) error {
	return gLogger.addlog(CRIT, facilitity, format, v...)
}

func (this *logger) getLogLvlStr(logType int) string {
	str := ""
	switch logType {
	case EMEGR:
		str = "[EMEGR]"
	case ALERT:
		str = "[ALERT]"
	case CRIT:
		str = "[CRIT]"
	case ERROR:
		str = "[ ERR]"
	case WARN:
		str = "[WARN]"
	case NOTICE:
		str = "[NOTICE]"
	case INFO:
		str = "[INFO]"
	case DEBUG:
		str = "[DEBUG]"
	case TRACE:
		str = "[TRACE]"
	default:
		str = "[DEBUG]"
	}
	return str
}

func (this *logger) getFacilityStr(facility int) string {
	str := ""
	switch facility {
	case FACILITY_SYS:
		str = "[  SYS]"
	case FACILITY_ADMIN:
		str = "[ADMIN]"
	case FACILITY_SEC:
		str = "[  SEC]"
	case FACILITY_APP:
		str = "[  APP]"
	case FACILITY_DB:
		str = "[   DB]"
	case FACILITY_GAME:
		str = "[ GAME]"
	case FACILITY_USER:
		str = "[ USER]"
	default:
		break
	}
	return str
}

func (this *logger) GetGoID() int32 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	id, err := strconv.Atoi(idField)
	if err != nil {
		panic(fmt.Sprintf("cannot get goroutine id: %v", err))
	}
	return int32(id)
}

func (this *logger) addlog(logLev int, facility int, format string, v ...interface{}) error {
	strLevel := this.getLogLvlStr(logLev)
	strFacility := this.getFacilityStr(facility)
	strGoID := fmt.Sprintf("[%05d]", this.GetGoID())

	strTime := this.getTime() + " "
	strFile := "[" + this.getFileLine() + "]"

	var msg string
	if format == "" {
		msg = fmt.Sprint(v...)
	} else {
		msg = fmt.Sprintf(format, v...)
	}

	strContent := fmt.Sprintf("%s%s%s", strGoID, strFile, msg)
	strLog := fmt.Sprintf("%s%s%s%s", strTime, strLevel, strFacility, strContent)

	if this.saveFlags&CONSOLE != 0 {
		fmt.Println(strLog)
	}

	if logLev > this.starLev {
		return nil
	}

	if this.saveFlags&REMOTE != 0 {
		if this.remoteLog != nil {
			this.remoteLog.put(uint64(time.Now().UnixNano()/10e5),
				uint8(facility), uint8(logLev), strContent)
		}
	}

	strLog += "\n"
	// this.logChan <- strLog
	this.put(strLog)

	return nil
}

// func (this *logger) popLog() *string {
// 	str := <-this.logChan
// 	return &str
// }

func (this *logger) put(data string) {
	lenData := len(data)
	if 0 == lenData {
		return
	}
	if lenData > max_dataLen_local{
		data = data[:max_dataLen_local]
	}

	this.msgQueueMutex.Lock()
	this.msgQueue = append(this.msgQueue, data)
	if len(this.msgQueue) > max_local_queue_size {
		this.msgQueue = this.msgQueue[1:]
	}
	this.msgQueueMutex.Unlock()
}

func (this *logger) pop_front() string {
	this.msgQueueMutex.Lock()
	defer this.msgQueueMutex.Unlock()
	if len(this.msgQueue) > 0 {
		d := this.msgQueue[0]
		this.msgQueue = this.msgQueue[1:]
		return d
	}
	return ""
}

