package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

func main() {
	// канал, в который кидаем все ошибки, чтобы показать их юзеру
	errChan := make(chan error, 10)

	// канал в который попадает открытый файл с данными
	waitChan := make(chan fyne.URIReadCloser, 1)

	app := app.New()
	window := app.NewWindow("Easymerch downloader")
	window.Resize(fyne.NewSize(600, 400)) // чтобы влезло меню выбора файла

	var vBox = container.NewVBox() // контейнер с вертикальным порядком дочерних виджетов

	input := widget.NewEntry()
	input.SetPlaceHolder("File path...")

	output := widget.NewMultiLineEntry()
	output.SetMinRowsVisible(10)

	errOutput := widget.NewMultiLineEntry()
	errOutput.SetMinRowsVisible(10)
	errOutput.SetText("Errors:\n")

	openFileBtn := widget.NewButton("Open", func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, window)
				return
			}
			if reader == nil {
				warn("Cancelled")
				return
			}

			input.SetText(reader.URI().Path())
			//openFileBtn.Disable() // Enable()
			waitChan <- reader

		}, window)

		// текущая директория, где запущена программа
		ex, err := os.Executable()
		if err == nil {

			exPath := filepath.Dir(ex)
			warn(exPath)

			// устанавливает директорию из которой, запущена программа, дефолтной при выборе файла
			curDir, err := storage.ListerForURI(storage.NewFileURI(exPath))
			if err == nil {
				fd.SetLocation(curDir)
			} else {
				warn(err)
			}
		} else {
			warn(err)
		}

		fd.SetFilter(storage.NewExtensionFileFilter([]string{".txt", ".csv", ".tsv", ".urls"}))
		fd.Show()
	})

	saveErrorsBtn := widget.NewButton("Save errors to file", func() {
		dialog.ShowFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, window)
				return
			}
			if writer == nil {
				warn("Cancelled")
				return
			}
			defer writer.Close()

			fileSaved(writer, window, errOutput)
		}, window)
	})

	fileDialog := container.NewGridWithColumns(2, input, openFileBtn)

	vBox.Add(fileDialog)

	vBox.Add(output)

	vBox.Add(errOutput)
	vBox.Add(saveErrorsBtn)

	vBox.Add(widget.NewButton("Quit", func() {
		app.Quit()
	}))

	window.SetContent(vBox) //устанавливаем контент для окна приложения

	go run(waitChan, 10, app, output, openFileBtn, errChan)

	go run2(errOutput, errChan)

	window.ShowAndRun() // запускаем сконфигурированное приложение
}

func fileSaved(f fyne.URIWriteCloser, w fyne.Window, errOutput *widget.Entry) {
	_, err := f.Write([]byte(errOutput.Text))
	if err != nil {
		dialog.ShowError(err, w)
	}

	err = f.Close()
	if err != nil {
		dialog.ShowError(err, w)
	}

	warn("Saved to...", f.URI())
}

func log1(a ...interface{}) {
	fmt.Fprintln(os.Stderr, a...)
}

func warn(a ...interface{}) {
	pc, file, line, ok := runtime.Caller(1)
	if ok {
		log1(fmt.Sprintf("%s[%s:%d]", runtime.FuncForPC(pc).Name(), file, line), a)
	} else {
		log1(a...)
	}
}

func run2(errOutput *widget.Entry, errChan chan error) {
	// ждём ошибки
	for e := range errChan {

		text := errOutput.Text

		lines := strings.Split(text, "\n")
		lines = append(lines, e.Error())
		errOutput.SetText(strings.Join(lines, "\n"))
	}
}

func run1(f fyne.URIReadCloser, num int, app fyne.App, output *widget.Entry, openFileBtn *widget.Button, errChan chan error) {
	if f == nil {
		warn("Cancelled")
		return
	}
	defer f.Close()

	openFileBtn.Disable()

	// запускаем воркеров для скачивания данных
	ch := make(chan [3]string, 1)
	for i := 0; i < num; i++ {
		go worker(ch, errChan)
	}

	dataDir := filepath.Join(filepath.Dir(f.URI().Path()), "images")

	// отправляем воркерам данные для скачивания
	r := csv.NewReader(f)
	r.Comma = '	'
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			errChan <- err
			warn(err)
			break
		}
		if len(record) < 2 {
			err = errors.New("Wrong nuber of columns: " + strings.Join(record, " | "))
			errChan <- err
			warn(err)
			break
		}
		//warn(record)

		url := record[0]
		filePath := record[1]

		text := output.Text

		lines := strings.Split(text, "\n")
		if len(lines) > 1 {
			lines = append(lines[1:], filepath.Base(url))
		}
		if len(lines) > 10 {
			lines = lines[1:]
		}
		lines = append([]string{"Downloading to " + dataDir + " ..."}, lines...)
		output.SetText(strings.Join(lines, "\n"))

		//output.SetText(strings.Join([]string{text, filepath.Base(url)}, "\n"))

		ch <- [3]string{url, filePath, dataDir}
	}

	output.SetText(output.Text + "\nDownload completed")

	close(ch)

	openFileBtn.Enable()

}

func run(waitChan chan fyne.URIReadCloser, num int, app fyne.App, output *widget.Entry, openFileBtn *widget.Button, errChan chan error) {
	// ждём, пока юзер выберет файл, из которого брать данные.
	for f := range waitChan {
		run1(f, num, app, output, openFileBtn, errChan)
	}
}

func worker(ch chan [3]string, errChan chan error) {
	for row := range ch {
		url := row[0]
		filePath := row[1]
		dataDir := row[2]
		warn(url, filePath, dataDir)

		dir := filepath.Join(dataDir, filepath.Dir(filePath))
		warn(dir)
		err := os.MkdirAll(dir, os.ModePerm)
		if err != nil && !os.IsExist(err) {
			errChan <- err
			warn(dir, err)
		} else {
			attemptNumber := 0
			for ; attemptNumber < 10; attemptNumber++ {
				err := urlToFile(url, filepath.Join(dir, filepath.Base(filePath)))
				if err == nil {
					break
				}
				errChan <- err
				warn(url, err)
				// ждём некоторое время и повторяем
				time.Sleep(time.Second * 10)
			}
			if attemptNumber >= 10 {
				err = errors.New("Can`t download url after 10 attempts. " + url)
				errChan <- err
				warn(err)
			}
		}
	}
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func urlToFile(url, name string) error {
	if fileExists(name) {
		return nil
	}

	// don't worry about errors
	response, err := http.Get(url)
	if err != nil {
		warn(err)
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		err = errors.New("Received non 200 response code for" + url)
		warn(err)
		return err
	}

	tmpName := name + ".tmp"

	//open a file for writing
	file, err := os.Create(tmpName)
	if err != nil {
		warn(err)
		return err
	}
	defer file.Close()

	// Use io.Copy to just dump the response body to the file. This supports huge files
	_, err = io.Copy(file, response.Body)
	if err != nil {
		warn(err)
		return err
	}

	err = file.Sync()
	if err != nil {
		warn(err)
		return err
	}

	err = file.Close()
	if err != nil {
		warn(err)
		return err
	}

	err = os.Rename(tmpName, name)
	if err != nil {
		warn(err)
		return err
	}

	return nil
}
