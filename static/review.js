const imgFolderSelect = document.getElementById('imgFolderSelect');
const jsonFileSelect = document.getElementById('jsonFileSelect');
const loadBtn = document.getElementById('loadBtn');
const currentImage = document.getElementById('currentImage');
const speakerInput = document.getElementById('speakerInput');
const textInput = document.getElementById('textInput');
const prevBtn = document.getElementById('prevBtn');
const nextBtn = document.getElementById('nextBtn');
const saveBtn = document.getElementById('saveBtn');
const indexDisplay = document.getElementById('indexDisplay');

let currentData = [];
let currentIndex = -1;
let currentJsonFilename = "";

// Init
init();

async function init() {
    loadFolders();
    loadJsonFiles();

    loadBtn.addEventListener('click', loadSession);
    prevBtn.addEventListener('click', () => nav(-1));
    nextBtn.addEventListener('click', () => nav(1));
    saveBtn.addEventListener('click', saveData);

    // Inputs listener to update local state immediately
    speakerInput.addEventListener('input', updateLocalState);
    textInput.addEventListener('input', updateLocalState);

    document.addEventListener('keydown', (e) => {
        if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
        if (e.key === 'ArrowLeft') nav(-1);
        if (e.key === 'ArrowRight') nav(1);
    });

    // Ctrl+S
    document.addEventListener('keydown', (e) => {
        if ((e.ctrlKey || e.metaKey) && e.key === 's') {
            e.preventDefault();
            saveData();
        }
    });

    // Debug image loading
    currentImage.onerror = () => {
        if (currentImage.src) {
            alert("Failed to load image: " + currentImage.src);
            console.error("Failed to load image:", currentImage.src);
        }
    };
}

async function loadFolders() {
    try {
        const res = await fetch('/api/folders');
        const folders = await res.json();

        imgFolderSelect.innerHTML = '';
        folders.forEach(f => {
            const opt = document.createElement('option');
            opt.value = f;
            opt.text = f;
            imgFolderSelect.add(opt);
        });
    } catch (e) { console.error(e); }
}

async function loadJsonFiles() {
    try {
        const res = await fetch('/api/conversations');
        const files = await res.json();

        jsonFileSelect.innerHTML = '';
        files.forEach(f => {
            const opt = document.createElement('option');
            opt.value = f;
            opt.text = f;
            jsonFileSelect.add(opt);
        });
    } catch (e) { console.error(e); }
}

async function loadSession() {
    const jsonFile = jsonFileSelect.value;
    if (!jsonFile) return;

    try {
        const res = await fetch(`/api/conversation/${encodeURIComponent(jsonFile)}`);
        currentData = await res.json();

        if (currentData && currentData.length > 0) {
            currentIndex = 0;
            currentJsonFilename = jsonFile;

            // Auto-detect folder from json filename
            // e.g. "v1/Chapter1.json" -> "v1/Chapter1"
            let derivedFolder = jsonFile.replace(/\.json$/i, "");
            // Select it in dropdown if exists, primarily for UI feedback
            // But we will use derivedFolder directly for images to be safe
            imgFolderSelect.value = derivedFolder;

            render();
        } else {
            alert("Empty or invalid file");
        }
    } catch (e) {
        console.error(e);
        alert("Error loading file");
    }
}

function render() {
    if (currentIndex < 0 || currentIndex >= currentData.length) return;

    const entry = currentData[currentIndex];

    // Use the dropdown value! This allows manual override if auto-detection fails.
    let folder = imgFolderSelect.value;

    // Fallback if dropdown is empty (e.g. invalid value selected), try derived
    if (!folder && currentJsonFilename) {
        folder = currentJsonFilename.replace(/\.json$/i, "");
    }

    currentImage.src = `/screenshots_view/${folder}/${entry.image_file}`;
    currentImage.style.display = 'block';

    speakerInput.value = entry.speaker || "";
    textInput.value = entry.text || "";

    indexDisplay.textContent = `${currentIndex + 1} / ${currentData.length}`;
}

function updateLocalState() {
    if (currentIndex < 0) return;
    currentData[currentIndex].speaker = speakerInput.value;
    currentData[currentIndex].text = textInput.value;
}

function nav(dir) {
    if (currentData.length === 0) return;

    const newIndex = currentIndex + dir;
    if (newIndex >= 0 && newIndex < currentData.length) {
        currentIndex = newIndex;
        render();
    }
}

async function saveData() {
    if (!currentJsonFilename || currentData.length === 0) return;

    try {
        const res = await fetch(`/api/conversation/${currentJsonFilename}`, {
            method: 'POST',
            body: JSON.stringify(currentData)
        });

        if (res.ok) {
            // Flash save indication?
            const originalText = saveBtn.textContent;
            saveBtn.textContent = "Saved!";
            setTimeout(() => saveBtn.textContent = originalText, 1000);
        } else {
            alert("Failed to save");
        }
    } catch (e) {
        console.error(e);
        alert("Error saving");
    }
}

function deleteEntry() {
    if (currentIndex < 0 || currentData.length === 0) return;
    if (!confirm("Delete this entry?")) return;

    currentData.splice(currentIndex, 1);

    if (currentData.length === 0) {
        currentIndex = -1;
        // Optionally clear UI
    } else if (currentIndex >= currentData.length) {
        currentIndex = currentData.length - 1;
    }

    render();
    saveData(); // Auto save
}

function insertBefore() {
    if (currentIndex < 0 && currentData.length > 0) currentIndex = 0;

    // Fallback if empty array?
    if (currentData.length === 0) {
        currentData.push({ image_file: "manual_insert", speaker: "", text: "" });
        currentIndex = 0;
    } else {
        // Insert empty entry copying image of current? or just manual?
        // Usually insert before means we missed a line for the SAME image or similar.
        // Let's copy current image file but empty text.
        const current = currentData[currentIndex];
        const newEntry = {
            image_file: current.image_file,
            speaker: "Unknown",
            text: ""
        };
        currentData.splice(currentIndex, 0, newEntry);
        // Current index stays same, which is now the NEW entry.
    }
    render();
    saveData(); // Auto save
}
