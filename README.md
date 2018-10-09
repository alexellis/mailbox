# mailbox

mailbox / dead-letter queue for OpenFaaS using NATS Streaming

Use-case: you want to process an asynchronous message but for some reason you cannot do that immediately. A common example may be when working with a remote web API which is rate-limited. This pattern allows for a back-off.

License: MIT

## Status

Working, but do not use in production without sufficent testing including the non-happy path.

## Setup

Example for Kubernetes:

https://github.com/stefanprodan/openfaas-colorisebot-gke-weave/blob/master/openfaas-fn/mailbox-dep.yaml

## Usage

See Colorisebot for an example usage:

https://github.com/alexellis/repaint-the-past/blob/ff2cf0677342f45f24a24fa28b70cdbbe5734a78/tweetpic/function/handler.py#L35

The mailbox runs on port 8080 and accepts (named) messages in the following format:

```
http://host:8080/deadletter/<message-type>
```

### Reference

| Header           | Purpose              |
|------------------|----------------------|
| X-Retries        | The count of retries already attempted. Increment on each attempt |
| X-Max-Retries    | Once `X-Retries` meets this value the message will be discarded, you can set this to an effectively infinite number if needed. |
| X-Delay-Duration | This should be an integer representing how long to wait between resubmitting the work - i.e. `5` for `5 seconds` |


### Python example

Example of submitting work to the mailbox (dead-letter queue):

```python
import requests

def requeue(st):
    # grab from headers or set defaults.
    retries = int(os.getenv("Http_X_Retries", "0"))
    max_retries = int(os.getenv("Http_X_Max_Retries", "9999")) # retry up to 9999
    delay_duration = int(os.getenv("Http_X_Delay_Duration", "60")) # delay 60s by default

    # Bump retries up one, since we're on a zero-based index.
    retries = retries + 1

    headers = {
        "X-Retries": str(retries),
        "X-Max-Retries": str(max_retries),
        "X-Delay-Duration": str(delay_duration)
    }

    r = requests.post("http://mailbox:8080/deadletter/tweetpic", data=st, json=False, headers=headers)

    print "Posting to Mailbox: ", r.status_code
    if r.status_code!= 202:
        print "Mailbox says: ", r.text

msg = "This is the message we can't process right now"
requeue(msg)
```

> Note: It is very important to set the appropriate headers as shown in the `headers` block on the HTTP request and then to increment the `X-Retries` variable for each re-submission.


