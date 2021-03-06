package view

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// HTMLEngine contains the html view engine structure.
type HTMLEngine struct {
	// files configuration
	directory string
	extension string
	assetFn   func(name string) ([]byte, error) // for embedded, in combination with directory & extension
	namesFn   func() []string                   // for embedded, in combination with directory & extension
	reload    bool                              // if true, each time the ExecuteWriter is called the templates will be reloaded, each ExecuteWriter waits to be finished before writing to a new one.
	// parser configuration
	options     []string // text options
	left        string
	right       string
	layout      string
	rmu         sync.RWMutex // locks for layoutFuncs and funcs
	layoutFuncs template.FuncMap
	funcs       template.FuncMap

	//
	middleware func(name string, contents []byte) (string, error)
	Templates  *template.Template
	//
}

var _ Engine = (*HTMLEngine)(nil)

var emptyFuncs = template.FuncMap{
	"yield": func(binding interface{}) (string, error) {
		return "", fmt.Errorf("yield was called, yet no layout defined")
	},
	"part": func() (string, error) {
		return "", fmt.Errorf("block was called, yet no layout defined")
	},
	"partial": func() (string, error) {
		return "", fmt.Errorf("block was called, yet no layout defined")
	},
	"partial_r": func() (string, error) {
		return "", fmt.Errorf("block was called, yet no layout defined")
	},
	"current": func() (string, error) {
		return "", nil
	},
	"render": func() (string, error) {
		return "", nil
	},
}

// HTML creates and returns a new html view engine.
// The html engine used like the "html/template" standard go package
// but with a lot of extra features.
// The given "extension" MUST begin with a dot.
func HTML(directory, extension string) *HTMLEngine {
	s := &HTMLEngine{
		directory:   directory,
		extension:   extension,
		assetFn:     nil,
		namesFn:     nil,
		reload:      false,
		left:        "{{",
		right:       "}}",
		layout:      "",
		layoutFuncs: make(template.FuncMap),
		funcs:       make(template.FuncMap),
	}

	return s
}

// Ext returns the file extension which this view engine is responsible to render.
func (s *HTMLEngine) Ext() string {
	return s.extension
}

// Binary optionally, use it when template files are distributed
// inside the app executable (.go generated files).
//
// The assetFn and namesFn can come from the go-bindata library.
func (s *HTMLEngine) Binary(assetFn func(name string) ([]byte, error), namesFn func() []string) *HTMLEngine {
	s.assetFn, s.namesFn = assetFn, namesFn
	return s
}

// Reload if set to true the templates are reloading on each render,
// use it when you're in development and you're boring of restarting
// the whole app when you edit a template file.
//
// Note that if `true` is passed then only one `View -> ExecuteWriter` will be render each time,
// no concurrent access across clients, use it only on development status.
// It's good to be used side by side with the https://github.com/kataras/rizla reloader for go source files.
func (s *HTMLEngine) Reload(developmentMode bool) *HTMLEngine {
	s.reload = developmentMode
	return s
}

// Option sets options for the template. Options are described by
// strings, either a simple string or "key=value". There can be at
// most one equals sign in an option string. If the option string
// is unrecognized or otherwise invalid, Option panics.
//
// Known options:
//
// missingkey: Control the behavior during execution if a map is
// indexed with a key that is not present in the map.
//	"missingkey=default" or "missingkey=invalid"
//		The default behavior: Do nothing and continue execution.
//		If printed, the result of the index operation is the string
//		"<no value>".
//	"missingkey=zero"
//		The operation returns the zero value for the map type's element.
//	"missingkey=error"
//		Execution stops immediately with an error.
//
func (s *HTMLEngine) Option(opt ...string) *HTMLEngine {
	s.rmu.Lock()
	s.options = append(s.options, opt...)
	s.rmu.Unlock()
	return s
}

// Delims sets the action delimiters to the specified strings, to be used in
// templates. An empty delimiter stands for the
// corresponding default: {{ or }}.
func (s *HTMLEngine) Delims(left, right string) *HTMLEngine {
	s.left, s.right = left, right
	return s
}

// Layout sets the layout template file which inside should use
// the {{ yield }} func to yield the main template file
// and optionally {{partial/partial_r/render}} to render other template files like headers and footers
//
// The 'tmplLayoutFile' is a relative path of the templates base directory,
// for the template file with its extension.
//
// Example: HTML("./templates", ".html").Layout("layouts/mainLayout.html")
//         // mainLayout.html is inside: "./templates/layouts/".
//
// Note: Layout can be changed for a specific call
// action with the option: "layout" on the iris' context.Render function.
func (s *HTMLEngine) Layout(layoutFile string) *HTMLEngine {
	s.layout = layoutFile
	return s
}

// AddLayoutFunc adds the function to the template's layout-only function map.
// It is legal to overwrite elements of the default layout actions:
// - yield func() (template.HTML, error)
// - current  func() (string, error)
// - partial func(partialName string) (template.HTML, error)
// - partial_r func(partialName string) (template.HTML, error)
// - render func(fullPartialName string) (template.HTML, error).
func (s *HTMLEngine) AddLayoutFunc(funcName string, funcBody interface{}) *HTMLEngine {
	s.rmu.Lock()
	s.layoutFuncs[funcName] = funcBody
	s.rmu.Unlock()
	return s
}

// AddFunc adds the function to the template's function map.
// It is legal to overwrite elements of the default actions:
// - url func(routeName string, args ...string) string
// - urlpath func(routeName string, args ...string) string
// - render func(fullPartialName string) (template.HTML, error).
// - tr func(lang, key string, args ...interface{}) string
func (s *HTMLEngine) AddFunc(funcName string, funcBody interface{}) *HTMLEngine {
	s.rmu.Lock()
	s.funcs[funcName] = funcBody
	s.rmu.Unlock()

	return s
}

// SetFuncs overrides the template funcs with the given "funcMap".
func (s *HTMLEngine) SetFuncs(funcMap template.FuncMap) *HTMLEngine {
	s.rmu.Lock()
	s.funcs = funcMap
	s.rmu.Unlock()

	return s
}

// Funcs adds the elements of the argument map to the template's function map.
// It is legal to overwrite elements of the map. The return
// value is the template, so calls can be chained.
func (s *HTMLEngine) Funcs(funcMap template.FuncMap) *HTMLEngine {
	s.rmu.Lock()
	for k, v := range funcMap {
		s.funcs[k] = v
	}
	s.rmu.Unlock()

	return s
}

// Load parses the templates to the engine.
// It's also responsible to add the necessary global functions.
//
// Returns an error if something bad happens, caller is responsible to handle that.
func (s *HTMLEngine) Load() error {
	// No need to make this with a complicated and "pro" way, just add lockers to the `ExecuteWriter`.
	// if `Reload(true)` and add a note for non conc access on dev mode.
	// atomic.StoreUint32(&s.isLoading, 1)
	// s.rmu.Lock()
	// defer func() {
	// 	s.rmu.Unlock()
	// 	atomic.StoreUint32(&s.isLoading, 0)
	// }()

	if s.assetFn != nil && s.namesFn != nil {
		// NOT NECESSARY "fix" of https://github.com/kataras/iris/issues/784,
		// IT'S BAD CODE WRITTEN WE KEEP HERE ONLY FOR A REMINDER
		// for any future questions.
		//
		// if strings.HasPrefix(s.directory, "../") {
		// 	// this and some more additions are fixes for https://github.com/kataras/iris/issues/784
		// 	// however, the dev SHOULD
		// 	// run the go-bindata command from the "$dir" parent directory
		// 	// and just use the ./$dir in the declaration,
		// 	// so all these fixes are not really necessary but they are here
		// 	// for the future
		// 	dir, err := filepath.Abs(s.directory)
		// 	// the absolute dir here can be invalid if running from other
		// 	// folder but we really don't care
		// 	// when we're working with the bindata because
		// 	// we only care for its relative directory
		// 	// see `loadAssets` for more.
		// 	if err != nil {
		// 		return err
		// 	}
		// 	s.directory = dir
		// }

		// embedded
		return s.loadAssets()
	}

	// load from directory, make the dir absolute here too.
	dir, err := filepath.Abs(s.directory)
	if err != nil {
		return err
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return err
	}

	// change the directory field configuration, load happens after directory has been set, so we will not have any problems here.
	s.directory = dir
	return s.loadDirectory()
}

// loadDirectory builds the templates from directory.
func (s *HTMLEngine) loadDirectory() error {
	dir, extension := s.directory, s.extension

	var templateErr error
	s.Templates = template.New(dir)
	s.Templates.Delims(s.left, s.right)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info == nil || info.IsDir() {
		} else {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				templateErr = err
				return err
			}

			ext := filepath.Ext(path)
			if ext == extension {

				buf, err := ioutil.ReadFile(path)
				if err != nil {
					templateErr = err
					return err
				}

				contents := string(buf)

				name := filepath.ToSlash(rel)
				tmpl := s.Templates.New(name)
				tmpl.Option(s.options...)

				if s.middleware != nil {
					contents, err = s.middleware(name, buf)
				}
				if err != nil {
					templateErr = err
					return err
				}
				// s.mu.Lock()
				// Add our funcmaps.
				_, err = tmpl.
					Funcs(emptyFuncs).
					//	Funcs(s.makeDefaultLayoutFuncs(name)).
					//	Funcs(s.layoutFuncs).
					Funcs(s.funcs).
					Parse(contents)
				// s.mu.Unlock()
				if err != nil {
					templateErr = err
					return err
				}
			}

		}
		return nil
	})

	if err != nil {
		return err
	}

	return templateErr
}

// loadAssets loads the templates by binary (go-bindata for embedded).
func (s *HTMLEngine) loadAssets() error {
	virtualDirectory, virtualExtension := s.directory, s.extension
	assetFn, namesFn := s.assetFn, s.namesFn

	var templateErr error
	s.Templates = template.New(virtualDirectory)
	s.Templates.Delims(s.left, s.right)
	names := namesFn()
	if len(virtualDirectory) > 0 {
		if virtualDirectory[0] == '.' { // first check for .wrong
			virtualDirectory = virtualDirectory[1:]
		}
		if virtualDirectory[0] == '/' || virtualDirectory[0] == os.PathSeparator { // second check for /something, (or ./something if we had dot on 0 it will be removed
			virtualDirectory = virtualDirectory[1:]
		}
	}

	for _, path := range names {
		// if filepath.IsAbs(virtualDirectory) {
		// 	// fixes https://github.com/kataras/iris/issues/784
		// 	// we take the absolute fullpath of the template file.
		// 	pathFileAbs, err := filepath.Abs(path)
		// 	if err != nil {
		// 		templateErr = err
		// 		continue
		// 	}
		//
		// 	path = pathFileAbs
		// }

		// bindata may contain more files than the templates
		// so keep that check as it's.
		if !strings.HasPrefix(path, virtualDirectory) {
			continue
		}

		ext := filepath.Ext(path)
		// check if extension matches
		if ext == virtualExtension {

			// take the relative path of the path as base of
			// virtualDirectory (the absolute path of the view engine that dev passed).
			rel, err := filepath.Rel(virtualDirectory, path)
			if err != nil {
				templateErr = err
				break
			}

			// // take the current working directory
			// cpath, err := filepath.Abs(".")
			// if err == nil {
			// 	// set the path as relative to "path" of the current working dir.
			// 	// fixes https://github.com/kataras/iris/issues/784
			// 	rpath, err := filepath.Rel(cpath, path)
			// 	// fix view: Asset  not found for path ''
			// 	if err == nil && rpath != "" {
			// 		path = rpath
			// 	}
			// }

			buf, err := assetFn(path)
			if err != nil {
				templateErr = fmt.Errorf("%v for path '%s'", err, path)
				break
			}

			contents := string(buf)
			name := filepath.ToSlash(rel)

			// name should be the filename of the template.
			tmpl := s.Templates.New(name)
			tmpl.Option(s.options...)

			if s.middleware != nil {
				contents, err = s.middleware(name, buf)
				if err != nil {
					templateErr = fmt.Errorf("%v for name '%s'", err, name)
					break
				}
			}

			// Add our funcmaps.
			if _, err = tmpl.Funcs(emptyFuncs).Funcs(s.funcs).Parse(contents); err != nil {
				templateErr = err
				break
			}
		}
	}
	return templateErr
}

func (s *HTMLEngine) executeTemplateBuf(name string, binding interface{}) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	err := s.Templates.ExecuteTemplate(buf, name, binding)

	return buf, err
}

func (s *HTMLEngine) layoutFuncsFor(lt *template.Template, name string, binding interface{}) {
	s.runtimeFuncsFor(lt, name, binding)

	funcs := template.FuncMap{
		"yield": func() (template.HTML, error) {
			buf, err := s.executeTemplateBuf(name, binding)
			// Return safe HTML here since we are rendering our own template.
			return template.HTML(buf.String()), err
		},
	}

	for k, v := range s.layoutFuncs {
		funcs[k] = v
	}

	lt.Funcs(funcs)
}

func (s *HTMLEngine) runtimeFuncsFor(t *template.Template, name string, binding interface{}) {
	funcs := template.FuncMap{
		"part": func(partName string) (template.HTML, error) {
			nameTemp := strings.Replace(name, s.extension, "", -1)
			fullPartName := fmt.Sprintf("%s-%s", nameTemp, partName)
			buf, err := s.executeTemplateBuf(fullPartName, binding)
			if err != nil {
				return "", nil
			}
			return template.HTML(buf.String()), err
		},
		"current": func() (string, error) {
			return name, nil
		},
		"partial": func(partialName string) (template.HTML, error) {
			fullPartialName := fmt.Sprintf("%s-%s", partialName, name)
			if s.Templates.Lookup(fullPartialName) != nil {
				buf, err := s.executeTemplateBuf(fullPartialName, binding)
				return template.HTML(buf.String()), err
			}
			return "", nil
		},
		// partial related to current page,
		// it would be easier for adding pages' style/script inline
		// for example when using partial_r '.script' in layout.html
		// templates/users/index.html would load templates/users/index.script.html
		"partial_r": func(partialName string) (template.HTML, error) {
			ext := filepath.Ext(name)
			root := name[:len(name)-len(ext)]
			fullPartialName := fmt.Sprintf("%s%s%s", root, partialName, ext)
			if s.Templates.Lookup(fullPartialName) != nil {
				buf, err := s.executeTemplateBuf(fullPartialName, binding)
				return template.HTML(buf.String()), err
			}
			return "", nil
		},
		"render": func(fullPartialName string) (template.HTML, error) {
			buf, err := s.executeTemplateBuf(fullPartialName, binding)
			return template.HTML(buf.String()), err
		},
	}

	t.Funcs(funcs)
}

// ExecuteWriter executes a template and writes its result to the w writer.
func (s *HTMLEngine) ExecuteWriter(w io.Writer, name string, layout string, bindingData interface{}) error {
	// re-parse the templates if reload is enabled.
	if s.reload {
		// locks to fix #872, it's the simplest solution and the most correct,
		// to execute writers with "wait list", one at a time.
		s.rmu.Lock()
		defer s.rmu.Unlock()
		if err := s.Load(); err != nil {
			return err
		}
	}

	t := s.Templates.Lookup(name)
	if t == nil {
		return fmt.Errorf("template: %s does not exist in the dir: %s", name, s.directory)
	}
	s.runtimeFuncsFor(t, name, bindingData)

	if layout = getLayout(layout, s.layout); layout != "" {
		lt := s.Templates.Lookup(layout)
		if lt == nil {
			return fmt.Errorf("layout: %s does not exist in the dir: %s", name, s.directory)
		}

		s.layoutFuncsFor(lt, name, bindingData)
		return lt.Execute(w, bindingData)
	}

	return t.Execute(w, bindingData)
}
