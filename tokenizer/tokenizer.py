import sentencepiece as spm

# 1. Wczytaj swój model
# Podaj ścieżkę do pliku .model, który sam wytrenowałeś
model_path = "./tokenizer/tokenizer_16384.model"
sp = spm.SentencePieceProcessor(model_file=model_path)

# 2. Twój tekst
text = "Learning tokenization with SentencePiece is efficient."

# 3. Zamiana na tokeny (ich identyfikatory)
tokens = sp.encode_as_ids(text)
liczba_tokenow = len(tokens)

# 4. Wynik i sprawdzenie limitu 256
print(f"Liczba tokenów: {liczba_tokenow}")

if liczba_tokenow > 256:
    print(f"⚠️ Za długi! Masz {liczba_tokenow} tokenów, a limit to 256. (Nadmiar: {liczba_tokenow - 256})")
else:
    print(f"✅ Zmieści się! Masz jeszcze {256 - liczba_tokenow} wolnych miejsc w kontekście.")

# Opcjonalnie: Zobacz jak dokładnie model "pociął" ten tekst
print("\nPodgląd podziału:")
print(sp.encode_as_pieces(text))