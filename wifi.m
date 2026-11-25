#import <CoreWLAN/CoreWLAN.h>
#include <net/if.h>
#include <net/if_dl.h>
#include <net/route.h>
#include <stdlib.h>
#include <string.h>
#include <sys/sysctl.h>
#include <sys/socket.h>
#include "wifi.h"

// SSID is not read: macOS requires location access for it.
void readWiFi(WiFiInfo *w) {
    memset(w, 0, sizeof *w);
    @autoreleasepool {
        CWInterface *i = CWWiFiClient.sharedWiFiClient.interface;
        if (!i) return;
        w->found = 1;
        w->power = i.powerOn;
        w->phy = (int)i.activePHYMode;
        w->rate = i.transmitRate;
        w->rssi = i.rssiValue;
        w->noise = i.noiseMeasurement;
        w->txPower = i.transmitPower;
        w->security = (int)i.security;
        CWChannel *c = i.wlanChannel;
        if (c) {
            w->channel = (int)c.channelNumber;
            w->band = (int)c.channelBand;
            w->width = (int)c.channelWidth;
        }
        strlcpy(w->name, i.interfaceName.UTF8String ?: "", sizeof w->name);
        strlcpy(w->country, i.countryCode.UTF8String ?: "", sizeof w->country);
    }
}

// IFLIST2 provides 64-bit counters (getifaddrs only 32-bit).
int readIfCounters(IfCounters *out, int max) {
    int mib[] = {CTL_NET, PF_ROUTE, 0, 0, NET_RT_IFLIST2, 0};
    size_t len = 0;
    if (sysctl(mib, 6, NULL, &len, NULL, 0)) return 0;
    len += len / 4;
    char *buf = malloc(len);
    if (!buf) return 0;
    int n = 0;
    if (!sysctl(mib, 6, buf, &len, NULL, 0)) {
        for (char *p = buf; p + sizeof(struct if_msghdr) <= buf + len && n < max;) {
            struct if_msghdr *h = (struct if_msghdr *)p;
            if (!h->ifm_msglen) break;
            if (h->ifm_type == RTM_IFINFO2) {
                struct if_msghdr2 *m = (struct if_msghdr2 *)p;
                char name[IF_NAMESIZE] = "";
                if (if_indextoname(m->ifm_index, name)) {
                    IfCounters *c = &out[n++];
                    strlcpy(c->name, name, sizeof c->name);
                    c->in = m->ifm_data.ifi_ibytes;
                    c->out = m->ifm_data.ifi_obytes;
                    c->speed = m->ifm_data.ifi_baudrate;
                    c->inErr = m->ifm_data.ifi_ierrors;
                    c->outErr = m->ifm_data.ifi_oerrors;
                }
            }
            p += h->ifm_msglen;
        }
    }
    free(buf);
    return n;
}
