#include <stdint.h>
typedef struct { char name[128]; double value; } TempReading;
int readTemps(TempReading *out, int max);
// Reads a numeric SMC key; returns 0 on success.
int smcOpen(void);
void smcClose(void);
int smcRead(const char *key, double *value);
