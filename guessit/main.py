from fastapi import FastAPI
from guessit import guessit

app = FastAPI()


@app.get("/health")
def health():
    return {"status": "ok"}


@app.post("/parse")
def parse(filename: str):
    return guessit(filename)


@app.post("/parse/batch")
def parse_batch(filenames: list[str]):
    return [guessit(f) for f in filenames]
