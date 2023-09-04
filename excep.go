package rlog

import (
	"fmt"
	"os"
	"runtime"
	"time"
)

const (
	maxStack  = 20
	separator = "---------------------------------------\n"
)

func HandlePanic(needLog bool, args ...interface{}) interface{} {
	if err := recover(); err != nil {
		errstr := fmt.Sprintf("%sruntime error: %v\ntraceback:\n", separator, err)

		i := 1
		for {
			pc, file, line, ok := runtime.Caller(i)
			errstr += fmt.Sprintf("    stack: %d %v [file: %s] [func: %s] [line: %d]\n", i, ok, file, runtime.FuncForPC(pc).Name(), line)

			i++
			if !ok || i > maxStack {
				break
			}
		}
		errstr += separator

		if len(args) > 0 {
			cb, ok := args[0].(func())
			if ok {
				defer func() {
					recover()
				}()
				cb()
			}
		}
		return err
	}
	return nil
}

func GetStackInfo() string {
	errstr := fmt.Sprintf("%straceback:\n", separator)

	i := 1
	for {
		pc, file, line, ok := runtime.Caller(i)
		errstr += fmt.Sprintf("    stack: %d %v [file: %s] [func: %s] [line: %d]\n", i, ok, file, runtime.FuncForPC(pc).Name(), line)

		i++
		if !ok || i > maxStack {
			break
		}
	}
	errstr += separator

	return errstr
}

func TryException() {
	errs := recover()
	if errs == nil {
		return
	}
	exeName := os.Args[0]

	now := time.Now() 
	pid := os.Getpid()

	time_str := now.Format("2006-01-02_15:04:05")                         
	fname := fmt.Sprintf("dump%s_%s_%v.log", exeName, time_str, pid)
	fmt.Println("dump to ", fname)

	f, err := os.Create(fname)
	if err != nil {
		return
	}
	defer f.Close()

	f.WriteString(fmt.Sprintf("%v\r\n", errs))
	f.WriteString("========\r\n")

	f.WriteString(GetStackInfo())
}

func RunCoroutine(cb func()) {
	go func() {
		defer TryException()
		cb()
	}()
}