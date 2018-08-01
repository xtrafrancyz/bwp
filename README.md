# *B*ackground *W*orker *P*ool

**BWP** can handle different jobs and proceed them in the background. This will be useful when you need to execute long-time request from php or any other *fast* context without possibility of waiting for a response.

Features:
- Job `http` -- Execute http request.
- Setup IP from which http requests will be sent.
- Graceful restart from updated binary


## Web API

#### `POST /post/http` -- Submit new http request
The method accepts raw json content in the request body with the following format:
```D
{
  "url": "https://google.com",
  "method": "GET", // Optional, GET by default
  "body": "base64 encoded raw body", // Optional
  "parameters": { // Optional, on GET or HEAD this will be appended to the url, othervise parameters will be in the POST args
    "foo": "bar"
  },
  "headers": { // Optional, any custom headers
    "X-Auth-Email": "example@example.com",
    "Cookie": "foo=bar"
  }
}
```
Multiple requests can also be sent at once using an array.

#### `GET /status` -- Get pool status (active workers, queue length, etc.)


## Configuration
- `-listen` addresses for binding a Web API, for multiple, separate with a comma
- `-pidfile` path to pid file
- `-pool-size` number of workers (default: 50)
- `-pool-queue-size` max number of jobs in queue (default: 10000)
- `-ip-routes` ip's from which http request will be sent (example: `172.16.0.0/12 -> 172.16.1.1, 0.0.0.0/0 -> auto`)
