const folderSelect = document.getElementById('folderSelect');
const outputName = document.getElementById('outputName');
const resizeFactor = document.getElementById('resizeFactor');
const resizeVal = document.getElementById('resizeVal');
const startBtn = document.getElementById('startBtn');
const stopBtn = document.getElementById('stopBtn');
const statusText = document.getElementById('statusText');
const conversationLog = document.getElementById('conversationLog');
const currentImage = document.getElementById('currentImage');

let pollingInterval = null;
let activeProcessingFolder = "";

// Init
resizeFactor.addEventListener('input', (e) => {
    resizeVal.textContent = e.target.value;
});

folderSelect.addEventListener('change', () => {
    const selected = folderSelect.value;
    if (selected) {
        // Split by / or \ to handle potential path variants, though server sends /
        const parts = selected.split(/[/\\]/);
        const leaf = parts[parts.length - 1];
        if (leaf) {
            outputName.value = leaf;
        }
    }
});



init();

async function init() {
    await loadFolders();
    checkServerStatus();
}

async function checkServerStatus() {
    try {
        const res = await fetch('/api/status');
        const status = await res.json();

        // Restore session data regardless of processing state
        const sessionRes = await fetch('/api/session');
        const sessionData = await sessionRes.json();
        if (sessionData && sessionData.length > 0) {
            updateUI(sessionData);
        }

        if (status.processing) {
            setProcessing(true);
            poll();
        }
    } catch (e) {
        console.error("Status check failed", e);
    }
}

async function loadFolders() {
    try {
        const res = await fetch('/api/folders');
        const folders = await res.json();

        folderSelect.innerHTML = '';
        if (folders.length === 0) {
            const opt = document.createElement('option');
            opt.text = "No folders found";
            opt.disabled = true;
            folderSelect.add(opt);
            return;
        }

        folders.forEach(f => {
            const opt = document.createElement('option');
            opt.value = f.path;

            let icon = "   ";
            if (f.processed) {
                if (f.entry_count !== f.image_count) {
                    icon = "⚠️ "; // Mismatch
                } else {
                    icon = "✅ "; // Success
                }
            }

            opt.text = icon + f.path + (f.processed ? ` (${f.entry_count}/${f.image_count})` : "");
            folderSelect.add(opt);
        });
    } catch (e) {
        console.error(e);
        statusText.textContent = "Error loading folders";
    }
}

startBtn.addEventListener('click', () => start(false));
document.getElementById('batchBtn').addEventListener('click', () => start(true));
stopBtn.addEventListener('click', stop);

// ...

async function start(isBatch) {
    let folder = folderSelect.value;
    // Enforce selection only for Single Mode
    if (!isBatch && !folder) return alert("Select a folder");

    // Disable buttons immediately
    startBtn.disabled = true;
    document.getElementById('batchBtn').disabled = true;

    const startData = {
        // Batch: Always empty (process all). Single: Selected folder.
        input_folder: isBatch ? "" : (folder ? "./screenshots/" + folder : ""),
        output_folder: "unused",
        resize_factor: parseFloat(resizeFactor.value),
        ai_backend: document.getElementById('aiBackend').value,
        model_id: document.getElementById('modelID').value,
        batch_mode: isBatch
    };

    try {
        const res = await fetch('/api/process/start', {
            method: 'POST',
            body: JSON.stringify(startData)
        });
        // ...

        if (res.ok) {
            setProcessing(true);
            poll();
        } else {
            alert("Failed to start: " + await res.text());
            setProcessing(false);
        }
    } catch (e) {
        console.error(e);
        alert("Error starting process");
        setProcessing(false);
    }
}

async function stop() {
    await fetch('/api/process/stop', { method: 'POST' });
    setProcessing(false);
}

function setProcessing(isProc) {
    startBtn.disabled = isProc;
    document.getElementById('batchBtn').disabled = isProc; // Fix: Re-enable batch button
    stopBtn.disabled = !isProc;
    folderSelect.disabled = isProc;
    if (isProc) {
        statusText.textContent = "Processing...";
        statusText.style.color = "var(--accent)";
    } else {
        statusText.textContent = "Idle";
        statusText.style.color = "var(--text-muted)";
        if (pollingInterval) clearInterval(pollingInterval);
    }
}

function poll() {
    if (pollingInterval) clearInterval(pollingInterval);
    pollingInterval = setInterval(async () => {
        // Check Status
        // const statusRes = await fetch('/api/status');
        // const status = await statusRes.json();
        // if (!status.processing) {
        //    setProcessing(false);
        // }

        // Get Session Data
        try {
            const res = await fetch('/api/session');
            const data = await res.json();
            updateUI(data);

            // If processing logic stops on server, backend `isProcessing` flag is cleared.
            const statusRes = await fetch('/api/status');
            const status = await statusRes.json();

            // Update Stats UI
            if (status.stats) {
                const s = status.stats;
                if (s.current_folder) activeProcessingFolder = s.current_folder;

                const progressBox = document.getElementById('statsContainer');
                progressBox.style.display = 'block';

                document.getElementById('statProgress').textContent = `${s.processed_images} / ${s.total_images}`;
                document.getElementById('statAvg').textContent = s.avg_time_per_img ? s.avg_time_per_img.toFixed(1) + 's' : '...';

                let etaStr = "...";
                if (s.eta) {
                    if (s.eta > 60) etaStr = (s.eta / 60).toFixed(1) + 'm';
                    else etaStr = s.eta.toFixed(0) + 's';
                }
                document.getElementById('statETA').textContent = etaStr;

                // Update Progress Bar
                if (s.total_images > 0) {
                    const pct = (s.processed_images / s.total_images) * 100;
                    document.getElementById('progressBar').style.width = pct + '%';
                }
            }

            if (!status.processing) {
                setProcessing(false);
                loadFolders(); // Refresh dropdown to show new status icons
            }

        } catch (e) {
            console.error(e);
        }

    }, 1000);
}

// Keep track of displayed count
let displayedCount = 0;

function updateUI(entries) {
    // If reset detected (e.g. new session started empty)
    if (entries.length < displayedCount) {
        conversationLog.innerHTML = '';
        displayedCount = 0;
    }

    // Append only new entries
    for (let i = displayedCount; i < entries.length; i++) {
        const entry = entries[i];
        const div = document.createElement('div');
        div.className = 'log-entry';
        div.innerHTML = `
            <div class="speaker">${entry.speaker || "Unknown"}</div>
            <div class="text">${entry.text}</div>
        `;
        conversationLog.appendChild(div);

        // Auto-scroll to bottom
        conversationLog.scrollTop = conversationLog.scrollHeight;
    }

    displayedCount = entries.length;

    if (entries.length > 0) {
        const last = entries[entries.length - 1];
        // Use active processing folder if available (from backend stats), otherwise dropdown
        const folder = activeProcessingFolder || folderSelect.value;
        const newSrc = `/screenshots_view/${folder}/${last.image_file}`;

        // Update image only if changed to avoid flickering
        if (currentImage.src !== newSrc && !currentImage.src.endsWith(newSrc)) {
            currentImage.src = newSrc;
            currentImage.style.display = 'block';
            const ph = document.getElementById('placeholderText');
            if (ph) ph.style.display = 'none';
        }
    }
}

