# Instrukcje Budowania (Build)

Ponieważ projekt jest aktualnie używany, oto komendy do samodzielnego zbudowania aplikacji po zakończeniu pracy:

**Wymagane:** Upewnij się, że jesteś w folderze projektu (`d:\!dev\Golang\VNDataPrepare`).

### 1. Capture App (Aplikacja do zrzutów)
Używa `syscall` dla myszki (bez CGO).

```powershell
go build -o capture.exe capture.go
```

### 2. server.exe (Web App)
Serwer obrabiający zdjęcia i wysyłający do AI.

```powershell
go build -o server.exe server.go
```

---
*Możesz skopiować i wkleić te komendy do terminala PowerShell.*
