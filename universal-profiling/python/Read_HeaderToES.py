import hashlib
from datetime import datetime
import logging
import multiprocessing
import re
import elasticapm
from ecs_logging import StdlibFormatter
from elasticsearch import Elasticsearch, helpers
import argparse
import os

clientapm = elasticapm.Client(
    service_name="ChessHeadersPuBSub",
    server_url="",
    secret_token="",
    environment="production",
    timeout="60s",
)
elasticapm.instrument()

ELASTIC_PASSWORD = os.getenv("ELASTIC_PASSWORD")
CLOUD_ID = ""
client = Elasticsearch(
    cloud_id=CLOUD_ID, basic_auth=("username", ELASTIC_PASSWORD), request_timeout=600
)


logger = logging.getLogger("chessToElasticsearch")
logger.setLevel(logging.DEBUG)
handler_stdout = logging.StreamHandler()
handler_stdout.setFormatter(StdlibFormatter())
logger.addHandler(handler_stdout)

@elasticapm.capture_span()
def index(docs):
    helpers.bulk(client, docs, chunk_size=5000, max_retries=100)
    return True


def read_header(games):
    try:
        docs = []
        today = datetime.today().isoformat() + "+00:00"
        clientapm.begin_transaction(transaction_type="request")
        for game in games:
            header_dict = {}
            for line in game.strip().split("\n"):
                if line.startswith("["):
                    key, value = line[1:-1].split(' "')
                    header_dict[key.strip()] = value.strip('"')
                elif line.startswith("1."):
                    header_dict["moves"] = line.strip()
            game_id = hashlib.sha256(game.encode()).hexdigest()
            timestamp = ""
            try:
                timestamp = (
                    datetime.strptime(
                        (header_dict["UTCDate"] + "T" + header_dict["UTCTime"]),
                        "%Y.%m.%dT%H:%M:%S",
                    ).isoformat()
                    + ".000+00:00"
                )
            except KeyError:
                logger.info("missing timestamp")
                if header_dict.get("UTCDate"):
                    timestamp = (
                        datetime.strptime(
                            (header_dict["UTCDate"] + "T12:00:00"),
                            "%Y.%m.%dT%H:%M:%S",
                        ).isoformat()
                        + ".000+00:00"
                    )
                else:
                    # let's place it in the future
                    timestamp = "2030-01-01T12:00:00.000+00:00"
            doc = {
                "_index": "chess-summaries",
                "_op_type": "create",
                "_source": {
                    "@timestamp": timestamp,
                    "db": "lichess",
                    "event": {"ingested": today},
                    "name": header_dict["Event"],
                    "game_id": game_id,
                    "url": header_dict["Site"],
                    "data_stream": {
                        "namespace": "default",
                        "type": "summary",
                        "dataset": "chess-games",
                    },
                    "user": {
                        "white": {
                            "name": header_dict["White"],
                            "elo": 0
                            if header_dict.get("WhiteElo") == "?"
                            else int(header_dict["WhiteElo"]),
                            "diff": int(header_dict.get("WhiteRatingDiff"))
                            if header_dict.get("WhiteRatingDiff")
                            else None,
                        },
                        "black": {
                            "name": header_dict["Black"],
                            "elo": 0
                            if header_dict.get("BlackElo") == "?"
                            else int(header_dict["BlackElo"]),
                            "diff": int(header_dict.get("BlackRatingDiff"))
                            if header_dict.get("BlackRatingDiff")
                            else None,
                        },
                    },
                    "termination": header_dict["Termination"].lower(),
                    "opening": {
                        "eco": header_dict["ECO"],
                        "name": header_dict["Opening"],
                    },
                    "timecontrol": header_dict["TimeControl"],
                    "result": {
                        "outcome": header_dict["Result"],
                        "white": True if header_dict["Result"] == "1-0" else None,
                        "black": True if header_dict["Result"] == "0-1" else None,
                        "draw": True if header_dict["Result"] == "1/2-1/2" else None,
                    },
                },
            }
            if header_dict["moves"].find("...") or header_dict["moves"].find("{"):
                # remove all the fancy stuff
                clean = re.sub("\{(.*?)\}", "", header_dict.get("moves"))
                clean = re.sub("(\d+\.{3})", "", clean)
                clean = re.sub("\?!|!\?|\?*|!*", "", clean)
                clean = re.sub("\s+", " ", clean)
                clean = re.sub(" (1\/2-1\/2|\d-\d)", "", clean)
                doc["_source"]["moves"] = {
                    "original": header_dict.get("moves"),
                    "clean": clean,
                    "total_moves": len(re.split("\d\. ", clean)) - 1,
                }
            docs.append(doc)
        transactionname = "Read header from %s games" % len(games)
        clientapm.end_transaction(transactionname, "success")
        clientapm.begin_transaction(transaction_type="request")
        if len(docs) > 0:
            index(docs)
        clientapm.end_transaction("Indexing", "success")
    except Exception as e:
        logger.error("Error with game: %s", e)


def read_games(file):
    with open(file) as f:
        i = 0
        clientapm.begin_transaction(transaction_type="request")
        tasks = []
        games = []
        temp_str = ""
        for line in f:
            if line.startswith("1."):
                temp_str += line
                i += 1
                games.append(temp_str)
                temp_str = ""
            elif line.startswith("["):
                temp_str += line
            elif line.strip() == "":
                temp_str += line
            if len(games) % 10000 == 0 and len(games) > 0:
                p = multiprocessing.Process(target=read_header, args=(games,))
                p.start()
                tasks.append(p)
                games = []
                clientapm.end_transaction("DiskRead 10.000", "success")
                clientapm.begin_transaction(transaction_type="request")
            if len(tasks) > 32:
                logger.info("waiting for all tasks to finish in %s", file)
                logger.info("Read %s games", i)
                for task in tasks:
                    task.join()
                logger.info("finished waiting")
                tasks = []
        if len(games) > 0:
            p = multiprocessing.Process(target=read_header, args=(games,))
            p.start()
            tasks.append(p)
    logger.info("waiting for all tasks to finish in %s", file)
    for task in tasks:
        task.join()
    logger.info("Done with waiting")
    return True


def get_games(
    file="lichess_db_standard_rated_2013-01.pgn",
):
    read_games(file)


def main():
    parser = argparse.ArgumentParser(description="Extract games from file")
    parser.add_argument("filename", type=str, help="File path to the games file")
    try:
        args = parser.parse_args()
        filename = args.filename
    except SystemExit:
        logger.info("No filename given, using default")
        filename = "lichess_db_standard_rated_2013-01.pgn"
    logger.info("starting with file: %s", filename)
    get_games(filename)
    logger.info("done with file: %s", filename)


if __name__ == "__main__":
    main()
