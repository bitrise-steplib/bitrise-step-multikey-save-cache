package step

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/bitrise-io/go-steputils/v2/cache"
	"github.com/bitrise-io/go-steputils/v2/stepconf"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
)

const (
	stepId       = "multikey-save-cache"
	uniquePrefix = "[u]"
	keyLimit     = 10 // max number of keys allowed
	pathLimit    = 10 // max number of paths allowed per key

	fmtErrParseInput               = "failed to parse inputs: %w"
	fmtErrNoKeyPathPairs           = "no key-path pairs found in input"
	fmtErrFailure                  = "save failed"
	fmtErrPartialFailure           = "save failures\n"
	fmtErrPartialFailureDetails    = "    - %s\n"
	fmtErrInvalidInput             = "invalid input (lines should follow the `KEY = PATH1, PATH2, ...` format): %s"
	fmtErrNoPathsFoundForKey       = "no paths found for key: %s"
	fmtErrPartialEvaluationFailure = "key-path pair evaluation failures\n"
	fmtErrEvaluationFailure        = "key-path pair evaluation failure: %w"

	fmtWarnSkippingAdditionalPaths = "Skipping additional paths for key '%s' as the limit of %d paths has been reached"
	fmtWarnSkippingAdditionalKeys  = "Skipping additional keys as the limit of %d keys has been reached"
)

type Input struct {
	Verbose          bool   `env:"verbose,required"`
	KeyPathPairs     string `env:"key_path_pairs,required"`
	CompressionLevel int    `env:"compression_level,range[1..19]"`
	CustomTarArgs    string `env:"custom_tar_args"`
}

type MultikeySaveCacheStep struct {
	logger         log.Logger
	inputParser    stepconf.InputParser
	commandFactory command.Factory
	pathChecker    pathutil.PathChecker
	pathProvider   pathutil.PathProvider
	pathModifier   pathutil.PathModifier
	envRepo        env.Repository
}

func New(logger log.Logger, inputParser stepconf.InputParser, commandFactory command.Factory, pathChecker pathutil.PathChecker, pathProvider pathutil.PathProvider, pathModifier pathutil.PathModifier, envRepo env.Repository) MultikeySaveCacheStep {
	return MultikeySaveCacheStep{
		logger:         logger,
		inputParser:    inputParser,
		commandFactory: commandFactory,
		pathChecker:    pathChecker,
		pathProvider:   pathProvider,
		pathModifier:   pathModifier,
		envRepo:        envRepo,
	}
}

func (step MultikeySaveCacheStep) Run() error {
	var input Input
	if err := step.inputParser.Parse(&input); err != nil {
		return fmt.Errorf(fmtErrParseInput, err)
	}
	stepconf.Print(input)
	step.logger.Println()

	step.logger.EnableDebugLog(input.Verbose)

	pathMap, uniquenessMap, evaluationError := input.evaluateKeyPairs(step.logger)
	if evaluationError != nil {
		return fmt.Errorf(fmtErrEvaluationFailure, evaluationError)
	}

	if len(pathMap) == 0 {
		return errors.New(fmtErrNoKeyPathPairs)
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(pathMap)) // buffered channel

	for key, paths := range pathMap {
		wg.Add(1)

		save(
			step,
			CacheInput{
				Verbose:          input.Verbose,
				Key:              key,
				Paths:            paths,
				IsKeyUnique:      uniquenessMap[key],
				CompressionLevel: input.CompressionLevel,
				CustomTarArgs:    input.CustomTarArgs,
			},
			&wg,
			errs,
		)
	}

	wg.Wait()
	close(errs)

	if len(errs) > 0 {
		step.logger.Printf(fmtErrPartialFailure)
		for err := range errs {
			step.logger.Printf(fmtErrPartialFailureDetails, err.Error())
		}
	}

	if len(errs) == len(pathMap) {
		return errors.New(fmtErrFailure)
	}

	return nil
}

func (input Input) evaluateKeyPairs(logger log.Logger) (map[string][]string, map[string]bool, error) {
	pathMap := make(map[string][]string)
	uniquenessMap := make(map[string]bool)
	var errs []error

	lines := strings.Split(input.KeyPathPairs, "\n")

	for idx, line := range lines {
		if idx >= keyLimit {
			logger.Warnf(fmtWarnSkippingAdditionalKeys, keyLimit)
			break
		}

		trimmedLine := strings.TrimSpace(line)

		var keyAndPaths = trimmedLine
		var isUnique = false
		if strings.HasPrefix(strings.TrimSpace(line), uniquePrefix) {
			keyAndPaths = trimmedLine[len(uniquePrefix):] // remove the prefix by slicing
			keyAndPaths = strings.TrimSpace(keyAndPaths)
			isUnique = true
		}

		keyPathParts := strings.SplitN(keyAndPaths, "=", 2)
		if len(keyPathParts) != 2 {
			err := fmt.Errorf(fmtErrInvalidInput, line)
			errs = append(errs, err)
			continue
		}

		key := strings.TrimSpace(keyPathParts[0])
		pathsString := strings.TrimSpace(keyPathParts[1])

		pathStrings := strings.Split(pathsString, ",")
		var paths []string
		for idx, pathString := range pathStrings {
			if idx >= pathLimit {
				logger.Warnf(fmtWarnSkippingAdditionalPaths, key, pathLimit)
				break
			}
			path := strings.TrimSpace(pathString)
			paths = append(paths, path)
		}

		if len(paths) == 0 {
			err := fmt.Errorf(fmtErrNoPathsFoundForKey, key)
			errs = append(errs, err)
			continue
		}

		pathMap[key] = paths
		uniquenessMap[key] = isUnique
	}

	if len(errs) > 0 {
		logger.Printf(fmtErrPartialEvaluationFailure)
		for _, err := range errs {
			logger.Printf(fmtErrPartialFailureDetails, err.Error())
		}
	}

	if len(errs) == len(lines) {
		return nil, nil, errors.New(fmtErrNoKeyPathPairs)
	}

	return pathMap, uniquenessMap, nil
}

type CacheInput struct {
	Verbose          bool     `env:"verbose,required"`
	Key              string   `env:"key,required"`
	Paths            []string `env:"paths,required"`
	IsKeyUnique      bool     `env:"is_key_unique"`
	CompressionLevel int      `env:"compression_level,range[1..19]"`
	CustomTarArgs    string   `env:"custom_tar_args"`
}

func save(
	step MultikeySaveCacheStep,
	cacheInput CacheInput,
	wg *sync.WaitGroup,
	errors chan<- error,
) {
	defer wg.Done()

	saver := cache.NewSaver(
		step.envRepo,
		step.logger,
		step.pathProvider,
		step.pathModifier,
		step.pathChecker,
		nil,
	)

	err := saver.Save(cache.SaveCacheInput{
		StepId:           stepId,
		Verbose:          cacheInput.Verbose,
		Key:              cacheInput.Key,
		Paths:            cacheInput.Paths,
		IsKeyUnique:      cacheInput.IsKeyUnique,
		CompressionLevel: cacheInput.CompressionLevel,
		CustomTarArgs:    strings.Fields(cacheInput.CustomTarArgs),
	})

	if err != nil {
		errors <- err
	}
}
