import gzip, json, sys, glob
def envelopes(path):
    data = gzip.open(path, 'rb').read()   # concatenated members -> one stream
    i = 0
    while i < len(data):
        j = data.index(b'\n', i)
        h = json.loads(data[i:j]); n = h['body_len']
        yield h, data[j+1:j+1+n]
        i = j + 1 + n
