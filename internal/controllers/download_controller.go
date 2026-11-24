package controllers

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/capimichi/spotiflac-rest-api/internal/models"
	"github.com/capimichi/spotiflac-rest-api/internal/repositories"
	"github.com/capimichi/spotiflac-rest-api/internal/services"
	"github.com/gin-gonic/gin"
)

type DownloadController struct {
	service *services.DownloadService
	repo    repositories.TaskRepository
}

func NewDownloadController(service *services.DownloadService, repo repositories.TaskRepository) *DownloadController {
	return &DownloadController{
		service: service,
		repo:    repo,
	}
}

// DownloadAsync godoc
// @Summary      Avvia download asincrono
// @Description  Avvia un task di download in background e restituisce immediatamente l'ID del task
// @Tags         Download
// @Accept       json
// @Produce      json
// @Param        request  body      models.DownloadRequest  true  "Parametri di download"
// @Success      202      {object}  map[string]interface{} "Contiene task_id, status e message"
// @Failure      400      {object}  map[string]string      "Richiesta malformata"
// @Router       /api/download [post]
func (ctrl *DownloadController) DownloadAsync(c *gin.Context) {
	var req models.DownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	setDefaults(&req)

	task, err := ctrl.service.DownloadAsync(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"task_id": task.ID,
		"status":  task.Status,
		"message": "Download task started successfully",
	})
}

// DownloadSync godoc
// @Summary      Avvia download sincrono
// @Description  Avvia il download e si blocca finché tutti i file non sono stati scaricati e taggati
// @Tags         Download
// @Accept       json
// @Produce      json
// @Param        request  body      models.DownloadRequest  true  "Parametri di download"
// @Success      200      {object}  map[string]interface{} "Contiene status e l'elenco dei file"
// @Failure      400      {object}  map[string]string      "Richiesta malformata"
// @Failure      500      {object}  map[string]string      "Errore durante il download"
// @Router       /api/download/sync [post]
func (ctrl *DownloadController) DownloadSync(c *gin.Context) {
	var req models.DownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	setDefaults(&req)

	files, err := ctrl.service.DownloadSync(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "completed",
		"files":  baseNames(files),
	})
}

// GetStatus godoc
// @Summary      Traccia lo stato del task
// @Description  Restituisce lo stato attuale di un download asincrono specificando l'ID del task
// @Tags         Download
// @Produce      json
// @Param        id   path      string  true  "ID del task"
// @Success      200  {object}  models.Task
// @Failure      404  {object}  map[string]string "Task non trovato"
// @Router       /api/status/{id} [get]
func (ctrl *DownloadController) GetStatus(c *gin.Context) {
	id := c.Param("id")
	task, exists, err := ctrl.repo.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}
	task.DownloadedFiles = nil // Never expose server filesystem paths.
	c.JSON(http.StatusOK, task)
}

// StreamAudio serves only a completed task's FLAC file. ServeContent honors HTTP Range requests.
func (ctrl *DownloadController) StreamAudio(c *gin.Context) {
	id := c.Param("id")
	task, exists, err := ctrl.repo.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not read task"})
		return
	}
	if !exists || task.Status != models.StatusCompleted {
		c.JSON(http.StatusNotFound, gin.H{"error": "Audio not found"})
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 || index >= len(task.DownloadedFiles) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Audio not found"})
		return
	}

	root, err := filepath.Abs("./downloads")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Audio unavailable"})
		return
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Audio not found"})
		return
	}
	path := task.DownloadedFiles[index]
	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(path)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Audio not found"})
			return
		}
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Audio not found"})
		return
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || !strings.EqualFold(filepath.Ext(path), ".flac") {
		c.JSON(http.StatusNotFound, gin.H{"error": "Audio not found"})
		return
	}
	file, err := os.Open(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Audio not found"})
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		c.JSON(http.StatusNotFound, gin.H{"error": "Audio not found"})
		return
	}

	c.Header("Content-Type", "audio/flac")
	c.Header("Content-Disposition", "inline")
	c.Header("Cache-Control", "private, no-store")
	c.Header("Accept-Ranges", "bytes")
	http.ServeContent(c.Writer, c.Request, filepath.Base(path), info.ModTime(), file)
}

func baseNames(files []string) []string {
	result := make([]string, 0, len(files))
	for _, file := range files {
		result = append(result, filepath.Base(file))
	}
	return result
}

func setDefaults(req *models.DownloadRequest) {
	if req.Service == "" {
		req.Service = "qobuz"
	}
	if req.Quality == "" {
		if req.Service == "qobuz" {
			req.Quality = "6"
		} else {
			req.Quality = "LOSSLESS"
		}
	}
	// Ignore caller-supplied paths; all output stays under the server-owned directory.
	req.OutputDir = "./downloads"
	if req.FilenameFormat == "" {
		req.FilenameFormat = "{artist} - {title}"
	}
}
