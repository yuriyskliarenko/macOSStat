#import <Foundation/Foundation.h>
#include <IOKit/IOKitLib.h>
#include <string.h>
#include "sensors.h"

// Undocumented HID API: temperatures without sudo (Apple Silicon).
typedef struct __IOHIDEvent *IOHIDEventRef;
typedef struct __IOHIDServiceClient *IOHIDServiceClientRef;
typedef struct __IOHIDEventSystemClient *IOHIDEventSystemClientRef;
extern IOHIDEventSystemClientRef IOHIDEventSystemClientCreate(CFAllocatorRef);
extern int IOHIDEventSystemClientSetMatching(IOHIDEventSystemClientRef, CFDictionaryRef);
extern CFArrayRef IOHIDEventSystemClientCopyServices(IOHIDEventSystemClientRef);
extern CFTypeRef IOHIDServiceClientCopyProperty(IOHIDServiceClientRef, CFStringRef);
extern IOHIDEventRef IOHIDServiceClientCopyEvent(IOHIDServiceClientRef, int64_t, int32_t, int64_t);
extern double IOHIDEventGetFloatValue(IOHIDEventRef, int32_t);

enum { kTempEvent = 15 };

int readTemps(TempReading *out, int max) {
    @autoreleasepool {
        IOHIDEventSystemClientRef client = IOHIDEventSystemClientCreate(kCFAllocatorDefault);
        if (!client) return 0;
        // 0xff00/5 are Apple temperature sensors
        NSDictionary *match = @{@"PrimaryUsagePage" : @0xff00, @"PrimaryUsage" : @5};
        IOHIDEventSystemClientSetMatching(client, (__bridge CFDictionaryRef)match);
        CFArrayRef services = IOHIDEventSystemClientCopyServices(client);
        int n = 0;
        for (CFIndex i = 0; services && i < CFArrayGetCount(services) && n < max; i++) {
            IOHIDServiceClientRef s = (IOHIDServiceClientRef)CFArrayGetValueAtIndex(services, i);
            CFTypeRef name = IOHIDServiceClientCopyProperty(s, CFSTR("Product"));
            IOHIDEventRef ev = IOHIDServiceClientCopyEvent(s, kTempEvent, 0, 0);
            if (name && ev && CFGetTypeID(name) == CFStringGetTypeID() &&
                CFStringGetCString(name, out[n].name, sizeof out[n].name, kCFStringEncodingUTF8)) {
                out[n++].value = IOHIDEventGetFloatValue(ev, kTempEvent << 16);
            }
            if (ev) CFRelease(ev);
            if (name) CFRelease(name);
        }
        if (services) CFRelease(services);
        CFRelease(client);
        return n;
    }
}

typedef struct {
    uint32_t key;
    struct { uint8_t major, minor, build, reserved; uint16_t release; } vers;
    struct { uint16_t version, length; uint32_t cpu, gpu, mem; } limits;
    struct { uint32_t size, type; uint8_t attr; } info;
    uint8_t result, status, data8;
    uint32_t data32;
    uint8_t bytes[32];
} SMCData;
_Static_assert(sizeof(SMCData) == 80, "SMC ABI");

static io_connect_t conn;

int smcOpen(void) {
    io_service_t s = IOServiceGetMatchingService(0, IOServiceMatching("AppleSMC"));
    if (!s) return -1;
    kern_return_t r = IOServiceOpen(s, mach_task_self(), 0, &conn);
    IOObjectRelease(s);
    return r == KERN_SUCCESS ? 0 : -1;
}

void smcClose(void) { IOServiceClose(conn); }

static int smcCall(SMCData *in, SMCData *out) {
    size_t size = sizeof *out;
    memset(out, 0, size);
    kern_return_t r = IOConnectCallStructMethod(conn, 2, in, sizeof *in, out, &size);
    return r == KERN_SUCCESS && out->result == 0 ? 0 : -1;
}

int smcRead(const char *key, double *value) {
    SMCData in = {0}, out;
    if (strlen(key) != 4) return -1;
    in.key = (uint32_t)(uint8_t)key[0] << 24 | (uint8_t)key[1] << 16 | (uint8_t)key[2] << 8 | (uint8_t)key[3];
    in.data8 = 9; // key info
    if (smcCall(&in, &out)) return -1;
    uint32_t size = out.info.size, type = out.info.type;
    in.info.size = size;
    in.data8 = 5; // read only
    if (smcCall(&in, &out)) return -1;
    uint8_t *b = out.bytes;
    if (type == 'flt ' && size == 4) { float f; memcpy(&f, b, 4); *value = f; }
    else if (type == 'fpe2' && size == 2) *value = (b[0] << 8 | b[1]) / 4.0;
    else if (type == 'sp78' && size == 2) *value = (int16_t)(b[0] << 8 | b[1]) / 256.0;
    else if (type == 'ui8 ' && size == 1) *value = b[0];
    else return -1;
    return 0;
}
