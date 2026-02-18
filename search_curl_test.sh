curl --request POST \
  --url https://api.firecrawl.dev/v2/search \
  --header "Authorization: Bearer $token" \
  --header 'Content-Type: application/json' \
  --data '
{
  "query": "Test",
  "limit": 3,
  "sources": [{"type": "web"}, {"type": "news"}, {"type": "images"}],
  "country": "US",
  "timeout": 60000,
  "ignoreInvalidURLs": false,
  "scrapeOptions": {
      "formats": [],
      "onlyMainContent": true,
      "maxAge": 172800000,
      "waitFor": 0,
      "mobile": false,
      "skipTlsVerification": false,
      "timeout": 30000,
      "parsers": [],
      "location": {
          "country": "US"
      },
      "removeBase64Images": true,
      "blockAds": true,
      "proxy": "auto",
      "storeInCache": true
  }
}
'