package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type DevDownloaderTest struct {
	manager *downloader.Manager
	repo    *repo.Repository
}

func NewDevDownloaderTest(manager *downloader.Manager, repo *repo.Repository) *DevDownloaderTest {
	return &DevDownloaderTest{
		manager: manager,
		repo:    repo,
	}
}

// RegisterDev registers dev-only routes outside the typed OpenAPI surface —
// shapes mirror raw downloader-client wire (arbitrary any). Only
// registered when cfg.Env == "dev".
func (h *DevDownloaderTest) RegisterDev(r chi.Router) {
	r.Get("/dev/downloader-test", h.ServeUI)
	r.Get("/dev/api/downloaders", h.ListDownloaders)
	r.Post("/dev/api/downloaders/{id}/add", h.AddMagnet)
	r.Get("/dev/api/downloaders/{id}/items", h.ListItems)
	r.Get("/dev/api/downloaders/{id}/items/{hash}", h.GetItem)
	r.Get("/dev/api/downloaders/{id}/items/{hash}/files", h.GetItemFiles)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *DevDownloaderTest) ServeUI(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
	<title>Downloader Test</title>
	<style>
		body {
			font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
			max-width: 1200px;
			margin: 0 auto;
			padding: 20px;
			background: #f5f5f5;
		}
		.container {
			background: white;
			padding: 20px;
			border-radius: 8px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
			margin-bottom: 20px;
		}
		h1 {
			margin-top: 0;
			color: #333;
		}
		.form-group {
			margin-bottom: 15px;
		}
		label {
			display: block;
			margin-bottom: 5px;
			font-weight: 500;
			color: #555;
		}
		select, input {
			width: 100%;
			padding: 8px 12px;
			border: 1px solid #ddd;
			border-radius: 4px;
			font-size: 14px;
			box-sizing: border-box;
		}
		button {
			background: #007bff;
			color: white;
			border: none;
			padding: 10px 20px;
			border-radius: 4px;
			cursor: pointer;
			font-size: 14px;
		}
		button:hover {
			background: #0056b3;
		}
		button:disabled {
			background: #ccc;
			cursor: not-allowed;
		}
		.items-list {
			margin-top: 20px;
		}
		.item {
			background: #f8f9fa;
			padding: 15px;
			margin-bottom: 10px;
			border-radius: 4px;
			border-left: 4px solid #007bff;
		}
		.item-header {
			display: flex;
			justify-content: space-between;
			align-items: center;
			margin-bottom: 10px;
		}
		.item-name {
			font-weight: 600;
			color: #333;
		}
		.item-status {
			padding: 4px 8px;
			border-radius: 4px;
			font-size: 12px;
			font-weight: 500;
		}
		.status-downloading { background: #17a2b8; color: white; }
		.status-seeding { background: #28a745; color: white; }
		.status-completed { background: #28a745; color: white; }
		.status-paused { background: #ffc107; color: #333; }
		.status-queued { background: #6c757d; color: white; }
		.status-errored { background: #dc3545; color: white; }
		.status-unknown { background: #6c757d; color: white; }
		.progress-bar {
			width: 100%;
			height: 20px;
			background: #e9ecef;
			border-radius: 10px;
			overflow: hidden;
			margin: 10px 0;
		}
		.progress-fill {
			height: 100%;
			background: #007bff;
			transition: width 0.3s;
		}
		.item-info {
			font-size: 12px;
			color: #666;
			margin-top: 5px;
		}
		.error {
			background: #f8d7da;
			color: #721c24;
			padding: 10px;
			border-radius: 4px;
			margin-bottom: 15px;
		}
		.refresh-btn {
			background: #6c757d;
			margin-left: 10px;
		}
		.refresh-btn:hover {
			background: #5a6268;
		}
		.status-badge {
			display: inline-block;
			padding: 2px 6px;
			border-radius: 3px;
			font-size: 11px;
			font-weight: 500;
			margin-left: 8px;
		}
		.status-active {
			background: #28a745;
			color: white;
		}
		.status-inactive {
			background: #dc3545;
			color: white;
		}
		.status-disabled {
			background: #6c757d;
			color: white;
		}
		.clients-list {
			margin-top: 15px;
			padding-top: 15px;
			border-top: 1px solid #e9ecef;
		}
		.client-item {
			display: flex;
			justify-content: space-between;
			align-items: center;
			padding: 8px 0;
			font-size: 13px;
		}
		.client-name {
			font-weight: 500;
		}
	</style>
</head>
<body>
	<div class="container">
		<h1>Downloader Test</h1>

		<div id="error" class="error" style="display: none;"></div>

		<div class="form-group">
			<label for="downloader">Downloader:</label>
			<select id="downloader">
				<option value="">Loading...</option>
			</select>
		</div>

		<div class="clients-list">
			<strong>Active Clients:</strong>
			<div id="clients-status">Loading...</div>
		</div>

		<div class="form-group">
			<label for="magnet">Magnet URL or Torrent File URL:</label>
			<input type="text" id="magnet" placeholder="magnet:?xt=urn:btih:... or https://example.com/file.torrent">
		</div>

		<button onclick="addMagnet()">Add Download (Magnet or Torrent URL)</button>
		<button class="refresh-btn" onclick="refreshItems()">Refresh</button>
	</div>

	<div class="container">
		<h2>Active Downloads</h2>
		<div id="items" class="items-list">
			<div>Select a downloader to view items</div>
		</div>
	</div>

	<script>
		let downloaderId = '';
		let refreshInterval = null;

		// Load downloaders on page load
		fetch('/dev/api/downloaders')
			.then(r => r.json())
			.then(data => {
				const select = document.getElementById('downloader');
				select.innerHTML = '<option value="">Select a downloader...</option>';

				// Update clients status
				const clientsDiv = document.getElementById('clients-status');
				const activeClients = data.filter(dl => dl.initialized);
				const inactiveClients = data.filter(dl => dl.enabled && !dl.initialized);
				const disabledClients = data.filter(dl => !dl.enabled);

				if (activeClients.length === 0 && inactiveClients.length === 0 && disabledClients.length === 0) {
					clientsDiv.innerHTML = '<div style="color: #666;">No downloaders configured</div>';
				} else {
					let html = '';
					if (activeClients.length > 0) {
						html += '<div style="margin-top: 8px;"><strong style="color: #28a745;">Active (' + activeClients.length + '):</strong>';
						activeClients.forEach(dl => {
							html += '<div class="client-item">' +
								'<span class="client-name">' + escapeHtml(dl.name) + '</span>' +
								'<span class="status-badge status-active">Active</span>' +
								'</div>';
						});
						html += '</div>';
					}
					if (inactiveClients.length > 0) {
						html += '<div style="margin-top: 8px;"><strong style="color: #dc3545;">Failed to Initialize (' + inactiveClients.length + '):</strong>';
						inactiveClients.forEach(dl => {
							html += '<div class="client-item">' +
								'<span class="client-name">' + escapeHtml(dl.name) + '</span>' +
								'<span class="status-badge status-inactive">Inactive</span>' +
								'</div>';
						});
						html += '</div>';
					}
					if (disabledClients.length > 0) {
						html += '<div style="margin-top: 8px;"><strong style="color: #6c757d;">Disabled (' + disabledClients.length + '):</strong>';
						disabledClients.forEach(dl => {
							html += '<div class="client-item">' +
								'<span class="client-name">' + escapeHtml(dl.name) + '</span>' +
								'<span class="status-badge status-disabled">Disabled</span>' +
								'</div>';
						});
						html += '</div>';
					}
					clientsDiv.innerHTML = html;
				}

				// Populate dropdown with status indicators
				data.forEach(dl => {
					const option = document.createElement('option');
					option.value = dl.id;
					let statusText = '';
					if (!dl.enabled) {
						statusText = ' [Disabled]';
					} else if (dl.initialized) {
						statusText = ' [Active]';
					} else {
						statusText = ' [Inactive]';
					}
					option.textContent = dl.name + ' (' + dl.type + ')' + statusText;
					select.appendChild(option);
				});

				select.onchange = function() {
					downloaderId = this.value;
					if (downloaderId) {
						refreshItems();
						startAutoRefresh();
					} else {
						stopAutoRefresh();
						document.getElementById('items').innerHTML = '<div>Select a downloader to view items</div>';
					}
				};
			})
			.catch(err => showError('Failed to load downloaders: ' + err));

		function showError(msg) {
			const errorDiv = document.getElementById('error');
			errorDiv.textContent = msg;
			errorDiv.style.display = 'block';
			setTimeout(() => {
				errorDiv.style.display = 'none';
			}, 5000);
		}

		function addMagnet() {
			const magnet = document.getElementById('magnet').value.trim();
			if (!magnet) {
				showError('Please enter a magnet URL or torrent file URL');
				return;
			}
			if (!downloaderId) {
				showError('Please select a downloader');
				return;
			}

			const btn = event.target;
			btn.disabled = true;
			btn.textContent = 'Adding...';

			fetch('/dev/api/downloaders/' + downloaderId + '/add', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ magnet: magnet })
			})
			.then(r => r.json())
			.then(data => {
				if (data.error) {
					showError(data.error);
				} else {
					document.getElementById('magnet').value = '';
					refreshItems();
				}
			})
			.catch(err => showError('Failed to add magnet: ' + err))
			.finally(() => {
				btn.disabled = false;
				btn.textContent = 'Add Download';
			});
		}

		function refreshItems() {
			if (!downloaderId) return;

			fetch('/dev/api/downloaders/' + downloaderId + '/items')
				.then(r => r.json())
				.then(data => {
					const itemsDiv = document.getElementById('items');
					if (data.error) {
						itemsDiv.innerHTML = '<div class="error">' + data.error + '</div>';
						return;
					}
					if (data.length === 0) {
						itemsDiv.innerHTML = '<div>No active downloads</div>';
						return;
					}

					itemsDiv.innerHTML = data.map(item => {
						const progress = Math.round((item.progress || 0) * 100);
						const status = item.status || 'unknown';
						const statusClass = 'status-' + status.toLowerCase();
						const name = escapeHtml(item.name || 'Unknown');
						const hash = item.externalId || 'N/A';
						const path = item.savePath ? 'Path: ' + escapeHtml(item.savePath) : '';
						return '<div class="item">' +
							'<div class="item-header">' +
							'<div class="item-name">' + name + '</div>' +
							'<div class="item-status ' + statusClass + '">' + status + '</div>' +
							'</div>' +
							'<div class="progress-bar">' +
							'<div class="progress-fill" style="width: ' + progress + '%"></div>' +
							'</div>' +
							'<div class="item-info">' +
							'Hash: ' + hash + ' | Progress: ' + progress + '% | ' + path +
							'</div>' +
							'</div>';
					}).join('');
				})
				.catch(err => {
					document.getElementById('items').innerHTML = '<div class="error">Failed to load items: ' + err + '</div>';
				});
		}

		function startAutoRefresh() {
			stopAutoRefresh();
			refreshInterval = setInterval(refreshItems, 5000);
		}

		function stopAutoRefresh() {
			if (refreshInterval) {
				clearInterval(refreshInterval);
				refreshInterval = null;
			}
		}

		function escapeHtml(text) {
			const div = document.createElement('div');
			div.textContent = text;
			return div.innerHTML;
		}
	</script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func (h *DevDownloaderTest) ListDownloaders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	downloaders, err := h.repo.ListDownloaders(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get list of initialized client IDs
	initializedClients := h.manager.ListClients(ctx)
	initializedIDs := make(map[string]bool)
	for _, client := range initializedClients {
		initializedIDs[string(client.InstanceID())] = true
	}

	result := make([]map[string]any, 0, len(downloaders))
	for _, dl := range downloaders {
		dlID := dl.ID.String()
		isInitialized := initializedIDs[dlID] && dl.Enabled

		result = append(result, map[string]any{
			"id":          dlID,
			"name":        dl.Name,
			"type":        dl.Type,
			"protocol":    dl.Protocol,
			"enabled":     dl.Enabled,
			"initialized": isInitialized,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

type AddMagnetRequest struct {
	Magnet string `json:"magnet"` // Can be a magnet: URL or http/https URL to a .torrent file
}

func (h *DevDownloaderTest) AddMagnet(w http.ResponseWriter, r *http.Request) {
	downloaderID := chi.URLParam(r, "id")
	if downloaderID == "" {
		writeJSONError(w, http.StatusBadRequest, "downloader ID required")
		return
	}

	var req AddMagnetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Magnet == "" {
		writeJSONError(w, http.StatusBadRequest, "magnet URL or torrent file URL required")
		return
	}

	ctx := r.Context()
	client, err := h.manager.GetClientByID(ctx, downloaderID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	addReq := downloader.AddRequest{
		MagnetURL: req.Magnet,
	}

	result, err := client.Add(ctx, addReq)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *DevDownloaderTest) ListItems(w http.ResponseWriter, r *http.Request) {
	downloaderID := chi.URLParam(r, "id")
	if downloaderID == "" {
		writeJSONError(w, http.StatusBadRequest, "downloader ID required")
		return
	}

	ctx := r.Context()
	client, err := h.manager.GetClientByID(ctx, downloaderID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	items, err := client.List(ctx)
	if err != nil {
		if errors.Is(err, downloader.ErrUnsupported) {
			writeJSONError(w, http.StatusNotImplemented, "List operation not supported by this downloader")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, items)
}

func (h *DevDownloaderTest) GetItem(w http.ResponseWriter, r *http.Request) {
	downloaderID := chi.URLParam(r, "id")
	hash := chi.URLParam(r, "hash")
	if downloaderID == "" || hash == "" {
		writeJSONError(w, http.StatusBadRequest, "downloader ID and hash required")
		return
	}

	ctx := r.Context()
	client, err := h.manager.GetClientByID(ctx, downloaderID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	item, err := client.Get(ctx, hash)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, item)
}

func (h *DevDownloaderTest) GetItemFiles(w http.ResponseWriter, r *http.Request) {
	downloaderID := chi.URLParam(r, "id")
	hash := chi.URLParam(r, "hash")
	if downloaderID == "" || hash == "" {
		writeJSONError(w, http.StatusBadRequest, "downloader ID and hash required")
		return
	}

	ctx := r.Context()
	client, err := h.manager.GetClientByID(ctx, downloaderID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	files, err := client.ListFiles(ctx, hash)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, files)
}
