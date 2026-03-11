import customtkinter as ctk
import cv2
import json
import os
from PIL import Image
import tkinter as tk
from tkinter import filedialog, messagebox

# --- CONFIG ---
ctk.set_appearance_mode("Dark")
ctk.set_default_color_theme("green")

class ModernVideoExtractor(ctk.CTk):
    def __init__(self):
        super().__init__()
        
        self.title("VN Video Extractor AI (Portrait Mode)")
        self.geometry("1400x950")
        
        # --- State Variables ---
        self.video_path = ""
        self.cap = None
        self.total_frames = 0
        self.current_frame_idx = 0
        self.selected_indices = set()
        self.is_video_loaded = False
        
        # Backend Config
        self.workspace_root = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
        self.screenshots_dir = os.path.join(self.workspace_root, "screenshots")
        self.conversation_dir = os.path.join(self.workspace_root, "conversation")
        
        self.roi_config = self.load_roi_from_config()
        self.game_name = ctk.StringVar(value="my_game")
        
        # --- Layout ---
        self.grid_columnconfigure(0, weight=1)
        self.grid_rowconfigure(0, weight=1)
        
        self.main_frame = ctk.CTkFrame(self, fg_color="transparent")
        self.main_frame.grid(row=0, column=0, sticky="nsew", padx=20, pady=20)
        self.main_frame.grid_columnconfigure(0, weight=3)
        self.main_frame.grid_columnconfigure(1, weight=1)
        self.main_frame.grid_rowconfigure(0, weight=1)
        
        # Left Panel (Video & Controls)
        self.left_panel = ctk.CTkFrame(self.main_frame)
        self.left_panel.grid(row=0, column=0, sticky="nsew", padx=(0, 10))
        self.left_panel.grid_rowconfigure(1, weight=1)
        self.left_panel.grid_columnconfigure(0, weight=1)
        
        # Info Header
        self.info_frame = ctk.CTkFrame(self.left_panel, fg_color="transparent")
        self.info_frame.grid(row=0, column=0, sticky="ew", pady=10, padx=10)
        
        roi_text = "ROI: Active" if self.roi_config else "ROI: None (Full Screen)"
        self.roi_label = ctk.CTkLabel(self.info_frame, text=roi_text, font=ctk.CTkFont(size=12, slant="italic"), text_color="gray")
        self.roi_label.pack(side="left")
        
        self.status_label = ctk.CTkLabel(self.info_frame, text="Select a video to begin...", font=ctk.CTkFont(size=14, weight="bold"))
        self.status_label.pack(side="left", padx=20)
        
        # Video Canvas
        # For portrait we need a taller canvas
        self.canvas_width = 540
        self.canvas_height = 960  # 9:16 portrait
        
        self.canvas_frame = ctk.CTkFrame(self.left_panel, fg_color="#111111")
        self.canvas_frame.grid(row=1, column=0, pady=10)
        
        self.canvas = tk.Canvas(self.canvas_frame, width=self.canvas_width, height=self.canvas_height, bg="#000000", highlightthickness=0)
        self.canvas.pack(padx=5, pady=5)
        
        # Controls
        self.control_frame = ctk.CTkFrame(self.left_panel)
        self.control_frame.grid(row=2, column=0, sticky="ew", pady=10, padx=10)
        
        btn_kwargs = {"width": 60, "height": 40, "font": ctk.CTkFont(weight="bold")}
        
        self.btn_load = ctk.CTkButton(self.control_frame, text="📁", command=self.load_video, width=50, height=40, font=ctk.CTkFont(size=20))
        self.btn_load.pack(side="left", padx=5)
        
        ctk.CTkButton(self.control_frame, text="<<", command=lambda: self.seek(-100), **btn_kwargs).pack(side="left", padx=2)
        ctk.CTkButton(self.control_frame, text="< 10", command=lambda: self.seek(-10), **btn_kwargs).pack(side="left", padx=2)
        ctk.CTkButton(self.control_frame, text="< 1", command=lambda: self.seek(-1), **btn_kwargs).pack(side="left", padx=2)
        
        self.btn_toggle = ctk.CTkButton(self.control_frame, text="MARK FRAME", command=self.toggle_selection, width=120, height=40, fg_color="#444444", hover_color="#555555", font=ctk.CTkFont(weight="bold"))
        self.btn_toggle.pack(side="left", padx=15)
        
        ctk.CTkButton(self.control_frame, text="1 >", command=lambda: self.seek(1), **btn_kwargs).pack(side="left", padx=2)
        ctk.CTkButton(self.control_frame, text="10 >", command=lambda: self.seek(10), **btn_kwargs).pack(side="left", padx=2)
        ctk.CTkButton(self.control_frame, text=">>", command=lambda: self.seek(100), **btn_kwargs).pack(side="left", padx=2)
        
        # Smart Search Panel
        self.smart_frame = ctk.CTkFrame(self.left_panel)
        self.smart_frame.grid(row=3, column=0, sticky="ew", pady=10, padx=10)
        
        ctk.CTkLabel(self.smart_frame, text="Smart ROI Diff Search:", font=ctk.CTkFont(weight="bold")).pack(side="left", padx=10)
        
        self.pixel_thresh_slider = ctk.CTkSlider(self.smart_frame, from_=10, to=1500, width=200)
        self.pixel_thresh_slider.set(200)
        self.pixel_thresh_slider.pack(side="left", padx=10)
        
        self.btn_smart = ctk.CTkButton(self.smart_frame, text="Find Next Change 🔍", command=self.seek_next_change, fg_color="#1b5e20", hover_color="#2e7d32", font=ctk.CTkFont(weight="bold"))
        self.btn_smart.pack(side="left", padx=10)
        
        self.pixel_result = ctk.CTkLabel(self.smart_frame, text="Diff: - px", text_color="#00e676", font=ctk.CTkFont(weight="bold"))
        self.pixel_result.pack(side="left", padx=10)
        
        # Right Panel (List & Export)
        self.right_panel = ctk.CTkFrame(self.main_frame, width=300)
        self.right_panel.grid(row=0, column=1, sticky="nsew")
        self.right_panel.grid_rowconfigure(3, weight=1)
        
        # Game Config (Presets & Folders)
        self.export_cfg_frame = ctk.CTkFrame(self.right_panel, fg_color="#222222")
        self.export_cfg_frame.grid(row=0, column=0, sticky="ew", padx=10, pady=(10, 0))
        
        ctk.CTkLabel(self.export_cfg_frame, text="Game Name (Preset):", font=ctk.CTkFont(weight="bold")).pack(pady=(5,2))
        self.game_entry = ctk.CTkEntry(self.export_cfg_frame, textvariable=self.game_name, width=200)
        self.game_entry.pack(pady=2)
        
        roi_btn_frame = ctk.CTkFrame(self.export_cfg_frame, fg_color="transparent")
        roi_btn_frame.pack(pady=5)
        ctk.CTkButton(roi_btn_frame, text="Load ROI", width=90, command=self.load_preset_roi).pack(side="left", padx=5)
        ctk.CTkButton(roi_btn_frame, text="Draw ROI", width=90, fg_color="#b8860b", hover_color="#daa520", command=self.start_roi_draw).pack(side="left", padx=5)

        ctk.CTkLabel(self.export_cfg_frame, text="Save To Sub-Folder:", font=ctk.CTkFont(weight="bold")).pack(pady=(10,2))
        
        self.save_dir_var = ctk.StringVar(value=self.game_name.get())
        folder_frame = ctk.CTkFrame(self.export_cfg_frame, fg_color="transparent")
        folder_frame.pack(pady=(0, 10))
        
        self.folder_entry = ctk.CTkEntry(folder_frame, textvariable=self.save_dir_var, width=150)
        self.folder_entry.pack(side="left", padx=5)
        ctk.CTkButton(folder_frame, text="📁", width=40, command=self.browse_output_dir).pack(side="left")
        
        # Sync the game name with folder name when typed unless user browsed manually
        self.game_name.trace_add("write", lambda *args: self.save_dir_var.set(self.game_name.get()))

        # Listbox
        ctk.CTkLabel(self.right_panel, text="Selected Frames", font=ctk.CTkFont(weight="bold", size=16)).grid(row=1, column=0, pady=(10, 5))
        
        # Using standard Tkinter Listbox for now
        self.list_frame = ctk.CTkFrame(self.right_panel, fg_color="#222222")
        self.list_frame.grid(row=2, column=0, sticky="nsew", padx=10, pady=5)
        
        self.listbox = tk.Listbox(self.list_frame, bg="#222222", fg="white", selectbackground="#1b5e20", bd=0, highlightthickness=0, font=("Arial", 11))
        self.scrollbar = ctk.CTkScrollbar(self.list_frame, command=self.listbox.yview)
        self.listbox.config(yscrollcommand=self.scrollbar.set)
        
        self.scrollbar.pack(side="right", fill="y", padx=2, pady=2)
        self.listbox.pack(side="left", fill="both", expand=True, padx=5, pady=5)
        self.listbox.bind('<<ListboxSelect>>', self.on_list_select)
        
        # Export Button
        self.btn_export = ctk.CTkButton(self.right_panel, text="EXPORT TO BACKEND 🚀", command=self.export_frames, height=50, fg_color="#aa0000", hover_color="#cc0000", font=ctk.CTkFont(size=15, weight="bold"), state="disabled")
        self.btn_export.grid(row=3, column=0, sticky="ew", padx=10, pady=20)
        
        # ROI Drawing Data
        self.is_drawing_roi = False
        self.roi_start = None
        self.roi_rect_id = None
        self.scale_factor = 1.0
        self.x_offset = 0
        self.y_offset = 0
        self.canvas.bind("<ButtonPress-1>", self.on_canvas_click)
        self.canvas.bind("<B1-Motion>", self.on_canvas_drag)
        self.canvas.bind("<ButtonRelease-1>", self.on_canvas_release)

    def start_roi_draw(self):
        if not self.is_video_loaded:
            messagebox.showinfo("Wait", "Please load a video first.")
            return
        self.is_drawing_roi = True
        self.status_label.configure(text="Click & Drag on Canvas to draw ROI")

    def on_canvas_click(self, event):
        if not self.is_drawing_roi: return
        self.roi_start = (event.x, event.y)
        if self.roi_rect_id:
            self.canvas.delete(self.roi_rect_id)
            
    def on_canvas_drag(self, event):
        if not self.is_drawing_roi or not self.roi_start: return
        if self.roi_rect_id:
            self.canvas.delete(self.roi_rect_id)
        self.roi_rect_id = self.canvas.create_rectangle(self.roi_start[0], self.roi_start[1], event.x, event.y, outline="red", width=2)
        
    def on_canvas_release(self, event):
        if not self.is_drawing_roi or not self.roi_start: return
        self.is_drawing_roi = False
        self.status_label.configure(text="ROI Saved for game preset.")
        
        x1, y1 = self.roi_start
        x2, y2 = event.x, event.y
        x_min, y_min = min(x1, x2), min(y1, y2)
        x_max, y_max = max(x1, x2), max(y1, y2)
        
        # Real coordinates mapping with offset included
        try:
            real_x = int((x_min - self.x_offset) / self.scale_factor)
            real_y = int((y_min - self.y_offset) / self.scale_factor)
            real_w = int((x_max - x_min) / self.scale_factor)
            real_h = int((y_max - y_min) / self.scale_factor)
            
            if real_w > 10 and real_h > 10:
                self.roi_config = {'x': max(0, real_x), 'y': max(0, real_y), 'w': real_w, 'h': real_h}
                self.save_preset_roi()
                self.roi_label.configure(text=f"ROI: Active ({real_w}x{real_h})")
                self.update_view()
        except Exception as e:
            self.status_label.configure(text="Invalid Draw Area.")

    def load_preset_roi(self):
        config_path = os.path.join(self.workspace_root, 'config.json')
        game = self.game_entry.get().strip()
        
        if os.path.exists(config_path):
            try:
                with open(config_path, 'r') as f:
                    data = json.load(f)
                    presets = data.get("roi_presets", {})
                    
                    if game in presets:
                        self.roi_config = presets[game]
                        self.roi_label.configure(text=f"ROI: Loaded for {game}")
                        self.update_view()
                        return
                    # Fallback to old global 'rect' if no preset
                    elif "rect" in data and not presets:
                        rect = data["rect"]
                        self.roi_config = {'x': rect['x'], 'y': rect['y'], 'w': rect['width'], 'h': rect['height']}
                        self.roi_label.configure(text="ROI: Loaded Global")
                        self.update_view()
                        return
                        
                messagebox.showinfo("ROI", f"No saved ROI preset found for game '{game}'.")
            except: pass
            
    def save_preset_roi(self):
        config_path = os.path.join(self.workspace_root, 'config.json')
        game = self.game_entry.get().strip()
        data = {}
        
        if os.path.exists(config_path):
            try:
                with open(config_path, 'r') as f:
                    data = json.load(f)
            except: pass
            
        if "roi_presets" not in data:
            data["roi_presets"] = {}
            
        data["roi_presets"][game] = self.roi_config
        
        # Save as main rect too for compatibility with golang capture
        data["rect"] = {
            "x": self.roi_config["x"],
            "y": self.roi_config["y"],
            "width": self.roi_config["w"],
            "height": self.roi_config["h"]
        }
        
        with open(config_path, 'w') as f:
            json.dump(data, f, indent=2)

    def load_roi_from_config(self):
        # We will load dynamically when Game name changes OR at start using the global
        config_path = os.path.join(self.workspace_root, 'config.json')
        if os.path.exists(config_path):
            try:
                with open(config_path, 'r') as f:
                    data = json.load(f)
                    # New Go Backend config format
                    if "rect" in data:
                        rect = data["rect"]
                        return {'x': rect['x'], 'y': rect['y'], 'w': rect['width'], 'h': rect['height']}
            except: pass
        return None

    def get_roi_crop(self, frame):
        if not self.roi_config: return frame
        x, y = int(self.roi_config['x']), int(self.roi_config['y'])
        w, h = int(self.roi_config['w']), int(self.roi_config['h'])
        h_img, w_img = frame.shape[:2]
        return frame[max(0,min(y,h_img)):min(y+h,h_img), max(0,min(x,w_img)):min(x+w,w_img)]

    def load_video(self):
        path = filedialog.askopenfilename(filetypes=[("Video", "*.mp4 *.mkv *.mov *.avi")])
        if not path: return
        
        if self.cap: self.cap.release()
        
        self.video_path = path
        self.cap = cv2.VideoCapture(path)
        self.total_frames = int(self.cap.get(cv2.CAP_PROP_FRAME_COUNT))
        self.current_frame_idx = 0
        self.selected_indices = set()
        self.is_video_loaded = True
        
        # Adjust Canvas orientation based on video orientation
        ret, frame = self.cap.read()
        if ret:
            h, w = frame.shape[:2]
            if w > h:
                # Landscape
                self.canvas_width = 1024
                self.canvas_height = 576
            else:
                # Portrait
                self.canvas_width = 540
                self.canvas_height = 960
            self.canvas.config(width=self.canvas_width, height=self.canvas_height)
        
        self.btn_export.configure(state="normal")
        self.update_view()
        self.update_sidebar()

    def seek(self, delta):
        if not self.is_video_loaded: return
        self.current_frame_idx = max(0, min(self.current_frame_idx + delta, self.total_frames - 1))
        self.update_view()

    def seek_next_change(self):
        if not self.is_video_loaded: return
        
        self.btn_smart.configure(text="Searching...", state="disabled")
        self.update()
        
        threshold_px = self.pixel_thresh_slider.get()
        
        self.cap.set(cv2.CAP_PROP_POS_FRAMES, self.current_frame_idx)
        ret, current_full = self.cap.read()
        if not ret: return
        
        current_gray = cv2.cvtColor(self.get_roi_crop(current_full), cv2.COLOR_BGR2GRAY)

        search_idx = self.current_frame_idx + 1
        found = False
        limit = min(self.total_frames, search_idx + 5000)
        step = 5  # Check every 5th frame for speed
        
        last_diff = 0

        while search_idx < limit:
            self.cap.set(cv2.CAP_PROP_POS_FRAMES, search_idx)
            ret, frame = self.cap.read()
            if not ret: break

            target_gray = cv2.cvtColor(self.get_roi_crop(frame), cv2.COLOR_BGR2GRAY)
            if current_gray.shape != target_gray.shape: break 

            diff = cv2.absdiff(current_gray, target_gray)
            _, diff_thresh = cv2.threshold(diff, 30, 255, cv2.THRESH_BINARY)
            changed_pixels = cv2.countNonZero(diff_thresh)
            last_diff = changed_pixels

            if changed_pixels > threshold_px:
                # Backtrack a bit to catch the exact moment
                self.current_frame_idx = max(self.current_frame_idx, search_idx + 2)
                found = True
                break
            
            search_idx += step
            if (search_idx % 30) == 0:
                self.pixel_result.configure(text=f"Diff: {changed_pixels} px")
                self.update()

        self.btn_smart.configure(text="Find Next Change 🔍", state="normal")
        
        if found:
            self.pixel_result.configure(text=f"Diff: {last_diff} px", text_color="#00e676")
            self.update_view()
        else:
            self.pixel_result.configure(text="No Changes Found", text_color="gray")

    def toggle_selection(self):
        if not self.is_video_loaded: return
        if self.current_frame_idx in self.selected_indices:
            self.selected_indices.remove(self.current_frame_idx)
        else:
            self.selected_indices.add(self.current_frame_idx)
        
        self.update_view()
        self.update_sidebar()

    def update_sidebar(self):
        self.listbox.delete(0, tk.END)
        for i, idx in enumerate(sorted(list(self.selected_indices))):
            self.listbox.insert(tk.END, f"{i+1}. Frame {idx}")
            if idx == self.current_frame_idx:
                self.listbox.selection_set(i)
                self.listbox.see(i)

    def on_list_select(self, event):
        sel = self.listbox.curselection()
        if sel: 
            try:
                frame_str = self.listbox.get(sel[0]).split("Frame ")[1]
                self.current_frame_idx = int(frame_str)
                self.update_view()
            except: pass

    def update_view(self):
        if not self.cap: return
        
        self.cap.set(cv2.CAP_PROP_POS_FRAMES, self.current_frame_idx)
        ret, frame = self.cap.read()
        if not ret: return
        
        # Convert BGR to RGB (PIL format)
        frame_rgb = cv2.cvtColor(frame, cv2.COLOR_BGR2RGB)
        
        # Draw ROI overlay on the RGB image directly (since we export cleanly later)
        if self.roi_config:
             x, y = int(self.roi_config['x']), int(self.roi_config['y'])
             w, h = int(self.roi_config['w']), int(self.roi_config['h'])
             cv2.rectangle(frame_rgb, (x, y), (x+w, y+h), (0, 255, 0), 3)

        # Resize to fit canvas
        h_f, w_f, _ = frame_rgb.shape
        self.scale_factor = min(self.canvas_width/w_f, self.canvas_height/h_f)
        new_w, new_h = int(w_f*self.scale_factor), int(h_f*self.scale_factor)
        
        frame_resized = cv2.resize(frame_rgb, (new_w, new_h))
        
        # We must use ctk.CTkImage for CustomTkinter
        pil_image = Image.fromarray(frame_resized)
        
        # Legacy Canvas backend for fast rendering
        from PIL import ImageTk
        self.photo = ImageTk.PhotoImage(image=pil_image)
        self.canvas.delete("all")
        
        # Calculate offset if canvas is larger than scaled image
        self.x_offset = (self.canvas_width - new_w) // 2
        self.y_offset = (self.canvas_height - new_h) // 2
        
        self.canvas.create_image(self.canvas_width//2, self.canvas_height//2, image=self.photo)
        
        if self.current_frame_idx in self.selected_indices:
            self.btn_toggle.configure(text="MARKED (Remove)", fg_color="#1b5e20", hover_color="#2e7d32")
        else:
            self.btn_toggle.configure(text="MARK FRAME", fg_color="#444444", hover_color="#555555")
            
        self.status_label.configure(text=f"Frame: {self.current_frame_idx} / {self.total_frames}")

    def browse_output_dir(self):
        # Open dialog in the screenshots directory
        init_dir = self.screenshots_dir if os.path.exists(self.screenshots_dir) else self.workspace_root
        folder = filedialog.askdirectory(initialdir=init_dir, title="Select Output Sub-Folder")
        if folder:
            # Try to get relative path from screenshots dir, or just use the basename
            rel = os.path.relpath(folder, self.screenshots_dir)
            if not rel.startswith(".."):
                self.save_dir_var.set(rel)
            else:
                self.save_dir_var.set(os.path.basename(folder))

    def export_frames(self):
        if not self.selected_indices: return
        
        game_name = self.game_entry.get().strip()
        sub_folder = self.save_dir_var.get().strip()
        
        if not game_name or not sub_folder:
            messagebox.showerror("Error", "Please enter both Game Name and Save Folder!")
            return
            
        # Structure:
        # screenshots/sub_folder/baten_xx.png
        # conversation/game_name.json
        
        target_img_dir = os.path.join(self.screenshots_dir, sub_folder)
        target_json_path = os.path.join(self.conversation_dir, f"{game_name}.json")
        
        os.makedirs(target_img_dir, exist_ok=True)
        os.makedirs(self.conversation_dir, exist_ok=True)
        
        self.btn_export.configure(text="EXPORTING...", state="disabled")
        self.update()
        
        sorted_idx = sorted(list(self.selected_indices))
        json_entries = []
        
        try:
            for i, idx in enumerate(sorted_idx):
                self.cap.set(cv2.CAP_PROP_POS_FRAMES, idx)
                ret, frame = self.cap.read()
                if ret:
                    # Save PNG file (generate safe filename based on time to avoid overwriting)
                    import datetime
                    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S_%f")[:22]
                    img_filename = f"{game_name}_{timestamp}.png"
                    img_path = os.path.join(target_img_dir, img_filename)
                    cv2.imwrite(img_path, frame)
                    
                    # Prepare info for json
                    # Format: sub_folder/game_name_timestamp.png
                    rel_path = f"{sub_folder}/{img_filename}"
                    
                    json_entries.append({
                        "image_file": rel_path,
                        "speaker": "",
                        "text": ""
                    })
                    
                self.status_label.configure(text=f"Exporting {i+1}/{len(sorted_idx)}...")
                self.update()
                
            # Avoid overwriting existing JSON with completed texts
            existing_entries = []
            if os.path.exists(target_json_path):
                try:
                    with open(target_json_path, 'r', encoding='utf-8') as f:
                        existing_entries = json.load(f)
                except: pass
            
            # Merge logic: Append new, skip existing duplicate filenames?
            if not existing_entries:    
                with open(target_json_path, 'w', encoding='utf-8') as f:
                    json.dump(json_entries, f, indent=2, ensure_ascii=False)
            else:
                # Add to existing
                existing_imgs = [e.get("image_file") for e in existing_entries]
                for n_entry in json_entries:
                    if n_entry["image_file"] not in existing_imgs:
                        existing_entries.append(n_entry)
                        
                with open(target_json_path, 'w', encoding='utf-8') as f:
                    json.dump(existing_entries, f, indent=2, ensure_ascii=False)
                    
            messagebox.showinfo("Success", f"Exported {len(sorted_idx)} frames to backend structure!\nPath: {target_img_dir}")
            
        except Exception as e:
            messagebox.showerror("Export Error", str(e))
        finally:
            self.btn_export.configure(text="EXPORT TO BACKEND 🚀", state="normal")
            self.update_view()

if __name__ == "__main__":
    app = ModernVideoExtractor()
    app.mainloop()
