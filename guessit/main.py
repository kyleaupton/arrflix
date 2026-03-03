from fastapi import FastAPI
from guessit import guessit

app = FastAPI()


@app.get("/health")
def health():
    return {"status": "ok"}


OPTS = {"single_value": True}


@app.post("/parse")
def parse(filename: str):
    return guessit(filename, OPTS)


@app.post("/parse/batch")
def parse_batch(filenames: list[str]):
    return [guessit(f, OPTS) for f in filenames]
