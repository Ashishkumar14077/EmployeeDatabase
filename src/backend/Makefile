#! /bin/bash

# Go parameters

EXE = backend

#Uncomment this line to update downloaded go packages in common/gocommon
#all: deps Build
all: Build

Build: 
        go build -o bin/$(EXE)

clean: 
        rm ./bin/$(EXE)

run:
        ./$(EXE)

deps:
        go get -d ./...
