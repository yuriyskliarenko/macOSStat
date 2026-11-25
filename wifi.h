#include <stdint.h>
typedef struct {
    int found, power, phy, channel, band, width, security;
    double rate;
    long rssi, noise, txPower;
    char name[32], country[8];
} WiFiInfo;
void readWiFi(WiFiInfo *w);

typedef struct { char name[32]; uint64_t in, out, speed, inErr, outErr; } IfCounters;
int readIfCounters(IfCounters *out, int max);
