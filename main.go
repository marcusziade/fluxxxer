package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/joho/godotenv"
)

type FluxInput struct {
	Prompt             string `json:"prompt"`
	Seed               *int   `json:"seed,omitempty"`
	NumOutputs         int    `json:"num_outputs"`
	AspectRatio        string `json:"aspect_ratio"`
	OutputFormat       string `json:"output_format"`
	OutputQuality      int    `json:"output_quality"`
	DisableSafetyCheck bool   `json:"disable_safety_checker"`
}

type App struct {
	*gtk.Application
	win       *gtk.ApplicationWindow
	entry     *gtk.Entry
	spinner   *gtk.Spinner
	imageBox  *gtk.Box
	statusBar *gtk.Label
}

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Error loading .env file: %v\n", err)
	}

	if os.Getenv("FLUX_API_URL") == "" {
		fmt.Fprintln(os.Stderr, "Error: FLUX_API_URL environment variable is not set")
		os.Exit(1)
	}

	app := &App{
		Application: gtk.NewApplication("com.example.flux", gio.ApplicationFlagsNone),
	}
	app.Application.ConnectActivate(app.setupUI)

	if code := app.Run(os.Args); code > 0 {
		os.Exit(code)
	}
}

func (a *App) setupUI() {
	a.win = gtk.NewApplicationWindow(a.Application)
	a.win.SetTitle("Fluxxxer")
	a.win.SetDefaultSize(2000, 600)

	mainBox := gtk.NewBox(gtk.OrientationVertical, 10)
	mainBox.SetMarginTop(10)
	mainBox.SetMarginBottom(10)
	mainBox.SetMarginStart(10)
	mainBox.SetMarginEnd(10)

	inputBox := gtk.NewBox(gtk.OrientationHorizontal, 5)
	a.entry = gtk.NewEntry()
	a.entry.SetPlaceholderText("Enter your prompt...")
	a.entry.SetHExpand(true)

	generateBtn := gtk.NewButtonWithLabel("Generate")
	generateBtn.ConnectClicked(a.onGenerateClicked)

	a.spinner = gtk.NewSpinner()

	inputBox.Append(a.entry)
	inputBox.Append(generateBtn)
	inputBox.Append(a.spinner)

	scrollWin := gtk.NewScrolledWindow()
	a.imageBox = gtk.NewBox(gtk.OrientationHorizontal, 10)
	scrollWin.SetChild(a.imageBox)
	scrollWin.SetVExpand(true)

	a.statusBar = gtk.NewLabel("")
	a.statusBar.SetXAlign(0)

	mainBox.Append(inputBox)
	mainBox.Append(scrollWin)
	mainBox.Append(a.statusBar)

	a.win.SetChild(mainBox)
	a.win.Show()
}

func (a *App) onGenerateClicked() {
	prompt := a.entry.Text()
	if prompt == "" {
		a.setStatus("Please enter a prompt")
		return
	}

	a.spinner.Start()
	a.clearImages()

	go func() {
		images, err := a.generateImages(prompt)
		glib.IdleAdd(func() {
			a.spinner.Stop()
			if err != nil {
				a.setStatus(fmt.Sprintf("Error: %v", err))
				return
			}
			a.displayImages(images)
		})
	}()
}

func (a *App) clearImages() {
	for child := a.imageBox.FirstChild(); child != nil; child = a.imageBox.FirstChild() {
		a.imageBox.Remove(child)
	}
}

func (a *App) generateImages(prompt string) ([]string, error) {
	apiURL := os.Getenv("FLUX_API_URL")
	if apiURL == "" {
		return nil, fmt.Errorf("FLUX_API_URL environment variable is not set")
	}

	input := FluxInput{
		Prompt:             prompt,
		NumOutputs:         4,
		AspectRatio:        "1:1",
		OutputFormat:       "png",
		OutputQuality:      1,
		DisableSafetyCheck: true,
	}

	payload := map[string]interface{}{"input": input}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(
		apiURL,
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var urls []string
	if err := json.NewDecoder(resp.Body).Decode(&urls); err != nil {
		return nil, err
	}

	return urls, nil
}

func (a *App) displayImages(urls []string) {
	for _, url := range urls {
		imageFrame := gtk.NewFrame("")
		imageBox := gtk.NewBox(gtk.OrientationVertical, 5)

		go func(url string) {
			texture, err := a.loadImageTexture(url)
			if err != nil {
				glib.IdleAdd(func() {
					a.setStatus(fmt.Sprintf("Error loading image: %v", err))
				})
				return
			}

			glib.IdleAdd(func() {
				picture := gtk.NewPicture()
				picture.SetPaintable(texture)
				picture.SetCanShrink(true)
				picture.SetHExpand(true)
				picture.SetVExpand(true)
				picture.SetContentFit(gtk.ContentFitContain)

				buttonBox := gtk.NewBox(gtk.OrientationHorizontal, 5)
				buttonBox.SetHAlign(gtk.AlignCenter)

				saveBtn := gtk.NewButtonWithLabel("Save")
				saveBtn.ConnectClicked(func() {
					a.saveImage(url)
				})

				copyBtn := gtk.NewButtonWithLabel("Copy")
				copyBtn.ConnectClicked(func() {
					a.copyImageToClipboard(texture)
				})

				buttonBox.Append(saveBtn)
				buttonBox.Append(copyBtn)

				imageBox.Append(picture)
				imageBox.Append(buttonBox)
				imageFrame.SetChild(imageBox)
				a.imageBox.Append(imageFrame)
			})
		}(url)
	}
}

func (a *App) saveImage(url string) {
	dialog := gtk.NewFileChooserNative(
		"Save Image",
		&a.win.Window,
		gtk.FileChooserActionSave,
		"_Save",
		"_Cancel",
	)

	defaultName := filepath.Base(url)
	if defaultName == "" || defaultName == "." {
		defaultName = "generated_image.png"
	}
	dialog.SetCurrentName(defaultName)

	filter := gtk.NewFileFilter()
	filter.AddPattern("*.png")
	filter.SetName("PNG images")
	dialog.AddFilter(filter)

	homeDir, err := os.UserHomeDir()
	if err == nil {
		picturesDir := filepath.Join(homeDir, "Pictures")
		if _, err := os.Stat(picturesDir); err == nil {
			gfile := gio.NewFileForPath(picturesDir)
			dialog.SetCurrentFolder(gfile)
		}
	}

	responseChan := make(chan int)
	dialog.ConnectResponse(func(response int) {
		responseChan <- response
	})

	dialog.Show()

	go func() {
		response := <-responseChan
		if response == int(gtk.ResponseAccept) {
			file := dialog.File()
			if file == nil {
				glib.IdleAdd(func() {
					a.setStatus("Error: No file selected")
				})
				return
			}

			path := file.Path()

			if !strings.HasSuffix(strings.ToLower(path), ".png") {
				path += ".png"
			}

			go func() {
				err := a.downloadAndSaveImage(url, path)
				glib.IdleAdd(func() {
					if err != nil {
						a.setStatus(fmt.Sprintf("Error saving image: %v", err))
					} else {
						a.setStatus(fmt.Sprintf("Image saved to: %s", path))
					}
				})
			}()
		}

		dialog.Destroy()
	}()
}

func (a *App) downloadAndSaveImage(url, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download image: status code %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(destPath), "*.png")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()

	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("failed to write image data: %w", err)
	}

	tmpFile.Close()

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to save image: %w", err)
	}

	return nil
}

func (a *App) loadImageTexture(url string) (*gdk.Texture, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	texture, err := gdk.NewTextureFromBytes(glib.NewBytesWithGo(data))
	if err != nil {
		return nil, err
	}

	return texture, nil
}

func (a *App) copyImageToClipboard(texture *gdk.Texture) {
	clipboard := gdk.DisplayGetDefault().Clipboard()
	clipboard.SetTexture(texture)
	a.setStatus("Image copied to clipboard")
}

func (a *App) setStatus(message string) {
	a.statusBar.SetText(message)
}
