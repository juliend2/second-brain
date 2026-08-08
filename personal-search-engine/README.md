README
======

# TODO

- [ ] Implement parsing for:
    - [x] txt
    - [x] word -- with [gomutex/godocx](https://github.com/gomutex/godocx)
    - [ ] excel -- with [qax-os/excelize](https://github.com/qax-os/excelize)
    - [ ] powerpoint
    - [x] pdf -- with [ledongthuc/pdf](github.com/ledongthuc/pdf)
    - [ ] (nicetohave) image (to index EXIF's `geopoint` at some point), for geo-targetted searches -- with [go-exiftool](https://github.com/barasher/go-exiftool)
- [ ] Implement crawling of:
    - [x] dropbox files
    - [x] google drive files
    - [>] notion documents
    - [ ] (nicetohave) todoist
    - [ ] (nicetohave) google calendar
    - [ ] (nicetohave) gmail
- [ ] Implement an indexer that:
    - [>] maps all the data sources (dropbox, notion, etc)
    - [x] finds out the file type
    - [x] execute the proper parsing strategy
- [ ] Implement a search engine
    - [x] with a CLI interface
    - [ ] with a Web UI

# FIXME

- [ ] Better handling of special cases
    - [ ] `’` to be matched by `'` as well
    - [ ] `d’Aquin` (with apostrophe) to be matched by `Aquin` as well (better tokenization)
    - [ ] `é` to be matched by `e` as well (and other accented characters)
- [ ] PDF:
    - [ ] Fix memory leak when parsing `crawling-strategies.pdf` (see fixtures/broken-files/)
- [ ] DOCX:
    - [ ] Fix parsing error when parsing cv.docx (see fixtures/broken-files/)



# Crawling

## rclone

supporte au moins google drive et dropbox.

### pour voir les fichiers sur mon drive.

specifiquement le fichier `impots 2025`:
```sh
rclone ls remote: | grep impots
```

### pour sync:
```sh
rclone sync remote: /home/julien/GoogleDrive --progress --exclude "/Eglise/**"
```
(où "remote" est le nom du remote que j'ai pris pour mon google drive)

(ça gère la conversion de google doc vers docx automatiquement)


# Indexing

[bleve](https://github.com/blevesearch/bleve)

## id for the index

`<source>-<path>`

where `source` is a slug of the source, such as:

- `db` for dropbox
- `gd` for google drive
- `nt` for notion

and where `path` is either an URL or a (relative) file path to the resource.

# Searching

[bleve](https://github.com/blevesearch/bleve)

