package api

import (
	"bytes"
	"flag"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var integrationCredentialsAvailable bool

func TestMain(m *testing.M) {
	var err error
	var testRoot string
	flag.Parse()

	err = godotenv.Load("../.env")
	if err != nil {
		log.Println("WARN: wasn't able to load env: ", err)
	}

	if os.Getenv("FSM_DIR") == "" || os.Getenv("FSM_CONF") == "" || os.Getenv("FSM_MODPACK_DIR") == "" {
		testRoot, err = os.MkdirTemp("", "factorio-server-manager-api-test-*")
		if err != nil {
			log.Fatalf("Error creating API test directory: %s", err)
		}
		factorioDir := filepath.Join(testRoot, "factorio")
		for _, directory := range []string{
			factorioDir,
			filepath.Join(factorioDir, "mods"),
			filepath.Join(factorioDir, "saves"),
			filepath.Join(testRoot, "modpacks"),
		} {
			if err := os.MkdirAll(directory, 0755); err != nil {
				log.Fatalf("Error creating API test directory %s: %s", directory, err)
			}
		}
		confPath := filepath.Join(testRoot, "conf.json")
		if err := os.WriteFile(confPath, []byte("{}\n"), 0600); err != nil {
			log.Fatalf("Error creating API test config: %s", err)
		}
		os.Setenv("FSM_DIR", factorioDir)
		os.Setenv("FSM_CONF", confPath)
		os.Setenv("FSM_MODPACK_DIR", filepath.Join(testRoot, "modpacks"))
		os.Setenv("mod_dir", filepath.Join(factorioDir, "mods"))
	}

	// basic setup stuff
	bootstrap.NewConfig([]string{
		"--dir", os.Getenv("FSM_DIR"),
		"--conf", os.Getenv("FSM_CONF"),
		"--mod-pack-dir", os.Getenv("FSM_MODPACK_DIR"),
		"--mod-dir", os.Getenv("mod_dir"),
	})

	factorio.SetFactorioServer(factorio.Server{
		Version:        factorio.Version{1, 1, 6, 0},
		BaseModVersion: "1.1.6",
	})

	if !testing.Short() {
		// Full integration tests may use explicitly supplied Factorio credentials.
		var cred factorio.Credentials
		load, err := cred.Load()
		if err != nil {
			log.Fatalf("Error loading factorio credentials: %s", err)
		}
		integrationCredentialsAvailable = load
		if !load && os.Getenv("factorio_username") != "" && os.Getenv("factorio_password") != "" {
			err, _ = factorio.FactorioLogin(os.Getenv("factorio_username"), os.Getenv("factorio_password"))
			if err != nil {
				log.Printf("Error logging in into factorio: %s", err)
			} else {
				integrationCredentialsAvailable = true
			}
		}
	}

	exitCode := m.Run()
	if testRoot != "" {
		if err := os.RemoveAll(testRoot); err != nil {
			log.Printf("WARN: could not remove API test directory: %s", err)
		}
	}
	os.Exit(exitCode)
}

func CheckShort(t *testing.T) {
	if testing.Short() {
		t.Skip("Do not run in Short-mode")
	}
	if !integrationCredentialsAvailable {
		t.Skip("Factorio portal integration credentials were not provided")
	}
}

func SetupMods(t *testing.T, empty bool) {
	var err error

	config := bootstrap.GetConfig()

	// check if dev directory exists and create it
	if _, err = os.Stat(config.FactorioModsDir); os.IsNotExist(err) {
		err = os.Mkdir(config.FactorioModsDir, 0775)
	}
	if err != nil {
		log.Fatalf(`Error creating "dev" directory: %s`, err)
		return
	}

	mod, err := factorio.NewMods(config.FactorioModsDir)
	if err != nil {
		t.Fatalf("couldn't create Mods object: %s", err)
	}

	if !empty {
		err := mod.DownloadMod("/download/belt-balancer/5fc1aca2bfe1b005c6943bf1", "belt-balancer_3.0.0.zip", "belt-balancer")
		if err != nil {
			t.Fatalf(`Error downloading Mod "belt-balancer": %s`, err)
		}

		err = mod.DownloadMod("/download/train-station-overview/5fc1b28cd3d1bb6fd86d9432", "train-station-overview_3.0.0.zip", "train-station-overview")
		if err != nil {
			t.Fatalf(`Error downloading Mod "train-station-overview": %s`, err)
		}

		err = mod.DownloadMod("/download/sonaxaton-infinite-resources/5dca095d440570000be0de82", "sonaxaton-infinite-resources_0.4.1.zip", "sonaxaton-infinite-resources")
		if err != nil {
			t.Fatalf(`Error downloading Mod "sonaxaton-infinite-resources": %s`, err)
		}
	}
}

func CleanupMods(t *testing.T) {
	config := bootstrap.GetConfig()
	err := os.RemoveAll(config.FactorioModsDir)
	if err != nil {
		t.Fatalf("Error removing dev directory: %s", err)
	}
}

// routeExpectation groups the response expectation for a route call: the HTTP
// status code and the optional expected JSON body. Grouping these related
// arguments keeps CallRoute under the authorized parameter limit.
type routeExpectation struct {
	statusCode int
	expected   string
}

func CallRoute(t *testing.T, method string, baseRoute string, route string, body io.Reader, handlerFunc http.HandlerFunc, expectation routeExpectation) {
	t.Helper()

	// create request to send
	request, err := http.NewRequest(method, route, body)
	if err != nil {
		t.Fatalf("Error creating request: %s", err)
	}

	// create response recorder
	recorder := httptest.NewRecorder()

	// get the handler, where the request is handled
	router := mux.NewRouter()
	router.HandleFunc(baseRoute, handlerFunc)

	// call the handler directly
	router.ServeHTTP(recorder, request)
	//handler.ServeHTTP(recorder, request)

	// status has to be 200
	if recorder.Code != expectation.statusCode {
		t.Fatalf("Wrong Status Code. expected %v - got %v", expectation.statusCode, recorder.Code)
	}

	if expectation.expected != "" {
		actual := recorder.Body.String()

		require.JSONEqf(t, expectation.expected, actual, `Wrong Body for route "%s". expected "%v" - actual "%v"`, route, expectation.expected, actual)
	}
}

// assertModEnabledState verifies that the named mod in the mods directory has
// the expected enabled state. It fails the test if the mod is missing or its
// state does not match, preserving the missing-mod failure and both enabled
// states for toggle checks.
func assertModEnabledState(t *testing.T, modsDir, modName string, enabled bool) {
	t.Helper()

	modList, err := factorio.NewMods(modsDir)
	if err != nil {
		t.Fatalf("Error creating Mods object: %s", err)
	}
	for _, mod := range modList.ModSimpleList.Mods {
		if mod.Name != modName {
			continue
		}
		if mod.Enabled != enabled {
			t.Fatalf("Mod %q enabled = %t; want %t", modName, mod.Enabled, enabled)
		}
		return
	}
	t.Fatalf("Mod %q not found", modName)
}

func ModEmptyBodyTest(t *testing.T, method string, route string, handlerFunc http.HandlerFunc) {
	t.Run("empty body", func(t *testing.T) {
		SetupMods(t, true)
		defer CleanupMods(t)

		CallRoute(t, method, route, route, nil, handlerFunc, routeExpectation{http.StatusBadRequest, ""})
	})
}

func ModInvalidJsonTest(t *testing.T, method, route string, handlerFunc http.HandlerFunc) {
	t.Run("invalid json body", func(t *testing.T) {
		SetupMods(t, true)
		defer CleanupMods(t)

		requestBody := strings.NewReader(`{"name": "asdc"`)

		CallRoute(t, method, route, route, requestBody, handlerFunc, routeExpectation{http.StatusBadRequest, ""})
	})
}

func ModNotExistTest(t *testing.T, method, route string, handlerFunc http.HandlerFunc) {
	t.Run("mod not exist", func(t *testing.T) {
		requestBody := strings.NewReader(`{"name": "lasdg"}`)

		CallRoute(t, method, route, route, requestBody, handlerFunc, routeExpectation{http.StatusInternalServerError, ""})
	})
}

func TestListInstalledModsHandler(t *testing.T) {
	CheckShort(t)

	SetupMods(t, false)
	defer CleanupMods(t)

	route := "/api/mods/list"

	expected := `[
  {
    "name": "belt-balancer",
    "version": "3.0.0",
    "title": "Belt Balancer",
    "author": "knoxfighter",
    "file_name": "belt-balancer_3.0.0.zip",
    "factorio_version": "1.1.0.0",
    "dependencies": null,
    "compatibility": true,
    "enabled": true
  },
  {
    "name": "sonaxaton-infinite-resources",
    "version": "0.4.1",
    "title": "Infinite Resources",
    "author": "sonaxaton",
    "file_name": "sonaxaton-infinite-resources_0.4.1.zip",
    "factorio_version": "0.17.0.0",
    "dependencies": null,
    "compatibility": false,
    "enabled": true
  },
  {
    "name": "train-station-overview",
    "version": "3.0.0",
    "title": "Train Station Overview",
    "author": "knoxfighter",
    "file_name": "train-station-overview_3.0.0.zip",
    "factorio_version": "1.1.0.0",
    "dependencies": null,
    "compatibility": true,
    "enabled": true
  }
]`

	CallRoute(t, "GET", route, route, nil, ListInstalledModsHandler, routeExpectation{http.StatusOK, expected})
}

func TestModToggleHandler(t *testing.T) {
	CheckShort(t)

	method := "POST"
	route := "/api/mods/toggle"
	handlerFunc := ModToggleHandler

	t.Run("success", func(t *testing.T) {
		SetupMods(t, false)
		defer CleanupMods(t)

		requestBody := strings.NewReader(`{"name": "belt-balancer"}`)

		// mod is now deactivated
		expected := "false"

		CallRoute(t, method, route, route, requestBody, handlerFunc, routeExpectation{http.StatusOK, expected})

		// check if changes happenes
		assertModEnabledState(t, bootstrap.GetConfig().FactorioModsDir, "belt-balancer", false)

		// toggle again, to check if the other direction also works
		// mod is now activated again
		expected = "true"

		// reset request body, it has to be red again
		requestBody.Seek(0, 0)

		CallRoute(t, method, route, route, requestBody, handlerFunc, routeExpectation{http.StatusOK, expected})

		assertModEnabledState(t, bootstrap.GetConfig().FactorioModsDir, "belt-balancer", true)
	})

	ModEmptyBodyTest(t, method, route, handlerFunc)

	ModInvalidJsonTest(t, method, route, handlerFunc)

	ModNotExistTest(t, method, route, handlerFunc)
}

func TestModDeleteHandler(t *testing.T) {
	CheckShort(t)

	method := "POST"
	route := "/api/mods/delete"
	handlerFunc := ModDeleteHandler

	t.Run("success", func(t *testing.T) {
		SetupMods(t, false)
		defer CleanupMods(t)

		requestBody := strings.NewReader(`{"name": "belt-balancer"}`)

		CallRoute(t, method, route, route, requestBody, handlerFunc, routeExpectation{http.StatusOK, `"belt-balancer"`})

		// check if mod is really not installed anymore
		config := bootstrap.GetConfig()
		modList, err := factorio.NewMods(config.FactorioModsDir)
		if err != nil {
			t.Fatalf("Error creating Mods object: %s", err)
		}
		if modList.ModSimpleList.CheckModExists("belt-balancer") {
			t.Fatalf("Mod is still installed, it should be gone by now")
		}
	})

	ModEmptyBodyTest(t, method, route, handlerFunc)

	ModInvalidJsonTest(t, method, route, handlerFunc)

	ModNotExistTest(t, method, route, handlerFunc)
}

func TestModDeleteAllHandler(t *testing.T) {
	CheckShort(t)

	method := "POST"
	route := "/api/mods/delete/all"
	handlerFunc := ModDeleteAllHandler

	t.Run("success", func(t *testing.T) {
		SetupMods(t, false)
		defer CleanupMods(t)

		CallRoute(t, method, route, route, nil, handlerFunc, routeExpectation{http.StatusOK, "null"})

		// check if no mods are there
		config := bootstrap.GetConfig()
		modList, err := factorio.NewMods(config.FactorioModsDir)
		if err != nil {
			t.Fatalf("Error creating mods object: %s", err)
		}
		if len(modList.ListInstalledMods().ModsResult) != 0 {
			t.Fatalf("Mods are still there!")
		}
	})
}

func TestModUpdateHandler(t *testing.T) {
	CheckShort(t)

	method := "POST"
	route := "/api/mods/update"
	handlerFunc := ModUpdateHandler

	requestBodySuccess := `{"modName": "belt-balancer", "downloadUrl": "/download/belt-balancer/5fc1aca2bfe1b005c6943bf1", "fileName": "belt-balancer_3.0.0.zip"}`

	t.Run("success", func(t *testing.T) {
		SetupMods(t, false)
		defer CleanupMods(t)

		expected := `{"name":"belt-balancer","version":"3.0.0","title":"Belt Balancer","author":"knoxfighter","file_name":"belt-balancer_3.0.0.zip","factorio_version":"1.1.0.0","dependencies":null,"compatibility":true,"enabled":true}`

		CallRoute(t, method, route, route, strings.NewReader(requestBodySuccess), handlerFunc, routeExpectation{http.StatusOK, expected})
	})

	t.Run("success with disabled mod", func(t *testing.T) {
		SetupMods(t, false)
		defer CleanupMods(t)

		// disable "belt-balancer" mod, so we can test, if it is still deactivated after
		config := bootstrap.GetConfig()
		modList, err := factorio.NewMods(config.FactorioModsDir)
		if err != nil {
			t.Fatalf("Error creating mods object: %s", err)
		}
		err, _ = modList.ModSimpleList.ToggleMod("belt-balancer")
		if err != nil {
			t.Fatalf("Error toggling mod: %s", err)
		}

		expected := `{"name":"belt-balancer","version":"3.0.0","title":"Belt Balancer","author":"knoxfighter","file_name":"belt-balancer_3.0.0.zip","factorio_version":"1.1.0.0","dependencies":null,"compatibility":true,"enabled":false}`

		CallRoute(t, method, route, route, strings.NewReader(requestBodySuccess), handlerFunc, routeExpectation{http.StatusOK, expected})
	})

	ModEmptyBodyTest(t, method, route, handlerFunc)

	ModInvalidJsonTest(t, method, route, handlerFunc)

	t.Run("mod not exist", func(t *testing.T) {
		SetupMods(t, false)
		defer CleanupMods(t)

		requestBody := strings.NewReader(`{"modName": "alfbasd", "downloadUrl": "/download/belt-balancer/5fc1aca2bfe1b005c6943bf1", "fileName": "belt-balancer_3.0.0.zip"}`)

		CallRoute(t, method, route, route, requestBody, handlerFunc, routeExpectation{http.StatusNotFound, ""})
	})

	t.Run("downloadUrl invalid", func(t *testing.T) {
		SetupMods(t, false)
		defer CleanupMods(t)

		requestBody := strings.NewReader(`{"modName": "belt-balancer", "downloadUrl": "/download/belt-balancer/bfe1b005c6943bf1", "fileName": "belt-balancer_3.0.0.zip"}`)

		CallRoute(t, method, route, route, requestBody, handlerFunc, routeExpectation{http.StatusInternalServerError, ""})
	})
}

// buildMultipartUploadRequest builds a multipart form request body for mod upload
// tests. If body is true and filePath is non-empty, the file is included as
// "mod_file". It returns the request body and the multipart content type. This
// helper is shared by the mod and modpack upload tests to avoid duplicated
// request-body construction.
func buildMultipartUploadRequest(t *testing.T, body bool, filePath string) (*bytes.Buffer, string) {
	t.Helper()

	requestBody := &bytes.Buffer{}
	writer := multipart.NewWriter(requestBody)

	if body && filePath != "" {
		file, err := os.Open(filePath)
		if err == nil {
			assert.NoError(t, err, "error opening mod file")

			formFile, err := writer.CreateFormFile("mod_file", filepath.Base(filePath))
			assert.NoError(t, err, "error creating formFileWriter")

			_, err = io.Copy(formFile, file)
			assert.NoError(t, err, "error copying file to form")

			file.Close()
		}
	}

	err := writer.Close()
	if err != nil {
		t.Fatalf("error closing the multipart writer: %s", err)
	}

	return requestBody, writer.FormDataContentType()
}

func ModUploadRequest(t *testing.T, body bool, filePath string) *httptest.ResponseRecorder {
	CheckShort(t)

	method := "POST"
	route := "/api/mods/upload"
	handlerFunc := ModUploadHandler

	requestBody, contentType := buildMultipartUploadRequest(t, body, filePath)

	request, err := http.NewRequest(method, route, requestBody)
	assert.NoError(t, err, "Error creating request")
	request.Header.Set("Content-Type", contentType)

	recorder := httptest.NewRecorder()

	handler := http.HandlerFunc(handlerFunc)

	handler.ServeHTTP(recorder, request)

	return recorder
}

func TestModUploadHandler(t *testing.T) {
	CheckShort(t)

	method := "POST"
	route := "/api/mods/upload"
	handlerFunc := ModUploadHandler

	t.Run("success", func(t *testing.T) {
		SetupMods(t, true)
		defer CleanupMods(t)

		recorder := ModUploadRequest(t, true, "../factorio_testfiles/belt-balancer_3.0.0.zip")

		// status has to be 200
		if recorder.Code != http.StatusOK {
			t.Fatalf("Wrong Status Code. expected %v - got %v", http.StatusOK, recorder.Code)
		}

		// check if mod is uploaded correctly
		config := bootstrap.GetConfig()
		modList, err := factorio.NewMods(config.FactorioModsDir)
		assert.NoError(t, err, "error creating mods object")

		expected := factorio.ModsResultList{
			ModsResult: []factorio.ModsResult{
				{
					ModInfo: factorio.ModInfo{
						Name:            "belt-balancer",
						Version:         "3.0.0",
						Title:           "Belt Balancer",
						Author:          "knoxfighter",
						FileName:        "belt-balancer_3.0.0.zip",
						FactorioVersion: factorio.Version{1, 1, 0, 0},
						Dependencies:    nil,
						Compatibility:   true,
					},
					Enabled: true,
				},
			},
		}

		actual := modList.ListInstalledMods()
		assert.Equal(t, expected, actual, `New mod is not correctly installed. expected "%v" - actual "%v"`, expected, actual)
	})

	ModEmptyBodyTest(t, method, route, handlerFunc)

	t.Run("empty file", func(t *testing.T) {
		SetupMods(t, true)
		defer CleanupMods(t)

		recorder := ModUploadRequest(t, true, "")
		assert.Equal(t, http.StatusBadRequest, recorder.Code, "wrong response code.")
	})

	t.Run("invalid mod file (txt-file)", func(t *testing.T) {
		SetupMods(t, true)
		defer CleanupMods(t)

		recorder := ModUploadRequest(t, false, "../factorio_testfiles/file_usage.txt")
		assert.Equal(t, http.StatusBadRequest, recorder.Code, "wrong response code.")
	})

	t.Run("invalid mod file (zip-file)", func(t *testing.T) {
		SetupMods(t, true)
		defer CleanupMods(t)

		recorder := ModUploadRequest(t, true, "../factorio_testfiles/invalid_mod.zip")
		assert.Equal(t, http.StatusInternalServerError, recorder.Code, "wrong response code.")
	})
}

func Test_018_10_Compatibility(t *testing.T) {
	CheckShort(t)

	var err error

	// set test environment to verson 1.0.0
	factorio.SetFactorioServer(factorio.Server{
		Version:        factorio.Version{1, 0, 0, 0},
		BaseModVersion: "1.0.0",
	})

	// when done reset the environment to what it should be
	defer factorio.SetFactorioServer(factorio.Server{
		Version:        factorio.Version{1, 1, 6, 0},
		BaseModVersion: "1.1.6",
	})

	// manually setup the environment, it has to be specific

	config := bootstrap.GetConfig()

	// check if dev directory exists and create it
	if _, err = os.Stat(config.FactorioModsDir); os.IsNotExist(err) {
		err = os.Mkdir(config.FactorioModsDir, 0775)
	}
	if err != nil {
		log.Fatalf(`Error creating "dev" directory: %s`, err)
		return
	}

	mod, err := factorio.NewMods(config.FactorioModsDir)
	if err != nil {
		t.Fatalf("couldn't create Mods object: %s", err)
	}

	err = mod.DownloadMod("/download/belt-balancer/5e9f9db4bf9d30000c5303f2", "belt-balancer_2.1.3.zip", "belt-balancer")
	if err != nil {
		t.Fatalf(`Error downloading Mod "belt-balancer": %s`, err)
	}

	defer CleanupMods(t)

	route := "/api/mods/list"

	expected := `[
  {
    "name": "belt-balancer",
    "version": "2.1.3",
    "title": "Belt Balancer",
    "author": "knoxfighter",
    "file_name": "belt-balancer_2.1.3.zip",
    "factorio_version": "0.18.0.0",
    "dependencies": null,
    "compatibility": true,
    "enabled": true
  }
]`

	CallRoute(t, "GET", route, route, nil, ListInstalledModsHandler, routeExpectation{http.StatusOK, expected})
}
