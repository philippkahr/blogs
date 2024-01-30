## Lichess games parser

Build and run it with

```bash
make
export ELASTIC_PASSWORD<elastic user password>
./lichess -file <path to PGN file>
```

This will ingest processed games into the Elastic stack using `CloudID`. 
