# stdprompt
Standardized prompt development, to API.

## Ideas
- [] support providing examples for testing a prompt
- [] Support a stdp "run", pick an example
- [] support the execution "traces" for a log. With "remote" storage?
- [] support adding examples/dataset to a prompt
- [] add a check command that verifies that
    - valid json schema for input/output
    - examples follow the schema
    - make sure the model(s) support the json schema
    - wether evaluators input match the output schema of the traces/schemas they are evaluating, how about evaluator versions.
- [] support different models
- [] allow serving as a http.Handler
- [] generate clients for all kinds of languages (based on openapi)
- [] support getting prompts, and passthrough completion (via openrouter?)
- [] support otel provider for observability (in the future we'll use this for an observability platform)
- [] be smarter in turning the llm's output into jsonschema, retry if not valid, or research BAML approach
- [] allow the server to be used as a mock that always returns the same data (for other systems to test against)
- [] vscode plugin for checking stdprompt files as they are saved
- [] support defining evaluators (LLM-as-a-judge)
- [] store trace data locally for tests, traces are matched to the prompt version (must be incremented manually?, or auto set to commit that changed it by ci/cd?)
- [] support a flow of "run" , read "input" from data files, and if output doesn't exist ask to update it in the data files.


## prior art
- [] https://github.com/google/dotprompt

## possible names
- langrest.com

## main oss usecase
- build in a container and deploy

## main saas usecase
- load prompt from github repo, serve on the edge (worlwide < latency>).
- caching headers for http clients.