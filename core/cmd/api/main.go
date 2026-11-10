package main

import (
	"log"
	"net/http"

	"docfinder/core/internal/config"
	"docfinder/core/internal/httpapi"
)

func main() {
	loadedConfig := config.Load()
	if loadedConfig.Debug {
		log.Printf("listen :%s mode %s ollama_configured %t", loadedConfig.Port, loadedConfig.Mode, loadedConfig.OllamaURL != "")
	}
	handler, handlerError := httpapi.NewHandler(loadedConfig)
	if handlerError != nil {
		log.Fatal(handlerError)
	}
	listenError := http.ListenAndServe(":"+loadedConfig.Port, handler)
	if listenError != nil {
		log.Fatal(listenError)
	}
}
