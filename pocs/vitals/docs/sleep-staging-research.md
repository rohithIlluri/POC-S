# Sleep staging from a WHOOP 4.0 strap: research and what vitals ships

Status: estimate only, unvalidated. No polysomnography (PSG) was available, so
the accuracy of anything here on this strap is **unknown**. This page says what
the literature supports, what can be reproduced with the strap's data, what was
built, and how it behaved on one real night (2026-10-21/22).

## 1. What the strap gives us (facts from the user's own strap)

| Signal | Rate / quality | Source |
|---|---|---|
| Heart rate (bpm) | one value per record, 0.9614 s apart (1.04 Hz) | v24 record byte 17 |
| RR intervals (ms) | up to 4 per record; mostly 1. In the sleep period they covered 89 % of elapsed time on the real night (whole recording: lower). Successive RRs across adjacent records differ by a median 28 ms, so they are consecutive beats | v24 bytes 18-26 |
| Gravity vector (g) | one per record | v24 bytes 36-47 |
| Movement value | one per record, unit unknown | v24 bytes 32-35 |
| Strap's own state | wake / still / sleep / up | v24 byte 80 high nibble |
| 25 Hz optical waveform | 38 s every ~20 min (about 3 % of the night), one channel | v25 records |
| Breathing rate | derived by us from RR (respiratory sinus arrhythmia) | `science.RespiratoryRate` |

The strap does **not** send sleep stages. In the APK, stages exist only as data
coming back from WHOOP's servers (`SleepActivity`: lightSleep, slowWaveSleep,
remSleep, wake, respiratoryRate; `SleepStagesModel` with an int `stage` written to
Android Health Connect). No BLE packet class carries a stage, hypnogram,
respiratory rate or skin temperature. See the findings file, Round 11.

## 2. Literature read for this work

All entries below were read at least to the abstract, via Europe PMC or the
publisher. Numbers are copied from those texts.

### Actigraphy (motion only)
- **Cole, Kripke, Gruen, Mullaney, Gillin 1992**, *Sleep* 15(5):461-469. Wrist
  activity scored sleep/wake correctly about 88 % of the time; sleep percentage
  and latency correlated 0.82 and 0.90 with PSG. Its coefficients are tied to the
  activity counts of a specific actigraph; the strap's movement value has no known
  unit, so the published formula cannot be applied faithfully. Not used.
- **Sadeh, Sharkey, Carskadon 1994**, *Sleep* 17(3):201-207. 91-93 % agreement
  with PSG; "agreement for sleep scoring was consistently higher than for wake
  scoring". Same count-unit problem. Not used.
- **van Hees et al. 2015**, *PLoS ONE* 10:e0142533 and **van Hees et al. 2018**,
  *Sci Rep* 8:12975. Arm-angle methods that need only a gravity vector; already
  implemented in `science/sleep.go` (the sleep period window). Reused here.
- Webster 1982 and Oakley 1997 were not read in this session and are not cited.

### Heart rate + motion and cardiorespiratory staging
- **Walch, Huang, Forger, Goldstein 2019**, *Sleep* 42(12):zsz180. Apple Watch
  acceleration + PPG heart rate, 31 subjects. Features: activity counts, the local
  standard deviation of heart rate (after a difference-of-Gaussians filter and
  per-subject normalisation), and a circadian "clock proxy". Sleep/wake about
  90 % accuracy, 93 % sensitivity, 59.6 % specificity; wake/NREM/REM best about
  72 % (kappa 0.28); on MESA 69 % (kappa 0.4). Code and data are public, trained
  model parameters are not in the paper. **Not reproducible as published here**;
  the idea "variable heart rate marks REM" is used.
- **Beattie et al. 2017**, *Physiol Meas* 38:1968-1979. Wrist PPG + accelerometer,
  60 adults, wake / light / deep / REM: 69 % accuracy, kappa 0.52 +/- 0.14; most
  errors light<->REM and light<->wake. Proprietary features and weights.
- **Fonseca et al. 2015**, *Physiol Meas* 36:2027-2040. ECG + respiratory effort,
  142 features, subject-specific z-score normalisation; 4-class kappa 0.49,
  accuracy 69 %. The per-subject z-scoring is used here.
- **Fonseca et al. 2017**, *Sleep* 40(7):zsx097. Wrist PPG HRV + accelerometer in
  healthy middle-aged adults: sleep/wake kappa 0.55; 3-class 72.9 % (kappa 0.46);
  4-class 59.3 % (kappa 0.42).
- **Radha et al. 2019**, *Sci Rep* 9:14149. LSTM on HRV, 584 nights: kappa
  0.61 +/- 0.15, 77 % accuracy; worse over age 50. Trained network, not published
  as weights. Not reproducible.
- **Willemen et al. 2014**, *IEEE JBHI* 18(2):661-669. SVM on cardiorespiratory +
  movement features: wake/sleep kappa 0.695, REM/NREM 0.558, N3 vs N1N2 0.553.
- **Kortelainen et al. 2010**, *IEEE TITB* 14(3):776-785. Bed-sensor heartbeat
  intervals + movement, HMM: wake/NREM/REM 79 % accuracy, kappa 0.43-0.44.
- **Altini & Kinnunen 2021**, *Sensors* 21:4302 (Oura). 4-stage 57 % with motion
  only, 79 % with autonomic and circadian features (5-fold CV).

### Physiology the rules lean on
- **Trinder et al. 2001**, *J Sleep Res* 10:253-264. NREM shifts autonomic balance
  to parasympathetic dominance; REM "in general similar to wakefulness"; heart rate
  also shows a time-of-night effect independent of stage.
- **Busek et al. 2005**, *Physiol Res* 54:369-376. Total HRV power and LF/HF highest
  in REM; HF higher in stage 2 and stage 4 than in REM; RR lengthens over cycles.
- **Douglas et al. 1982**, *Thorax* 37:840-844. Breathing in all sleep stages is more
  rapid and shallow than awake; ventilation lowest in REM.
- **Dijk 2009**, *J Clin Sleep Med* 5(2 Suppl):S6-S15. NREM-REM cycles of about
  80-120 min; REM about 18-25 % of sleep; slow-wave activity "rise rate and maximum
  level ... decline across consecutive NREM episodes". (The common statement that REM
  periods lengthen towards morning was not found in a source read in this session;
  it is used as a check, not as a claim.)
- **Ohayon et al. 2004**, *Sleep* 27(7):1255-1273. Normative meta-analysis: in adults,
  slow-wave %, REM % and REM latency fall with age.

### Independent validation of consumer wearables vs PSG
- **Miller et al. 2020**, *J Sports Sci* 38:2631-2636 (WHOOP, 12 adults, 86 sleeps):
  2-stage agreement 89 %, kappa 0.49, wake specificity 51 %; 4-stage agreement 64 %,
  kappa 0.47 (light 62 %, SWS 68 %, REM 70 %, wake 51 %).
- **Miller et al. 2022**, *Sensors* 22:6317 (53 adults, six devices): 2-state 86-89 %
  (kappa 0.30-0.51; WHOOP 3.0 86 %, 0.44); multi-state 50-65 % (kappa 0.20-0.52;
  WHOOP 60 %, 0.44; Oura 61 %, 0.43; Apple 53 %, 0.20).
- **Chinoy et al. 2021**, *Sleep* 44(5):zsaa291 (seven devices, 34 adults): epoch
  sensitivity >= 0.93, specificity 0.18-0.54; "device sleep stage assessments were
  inconsistent"; worse on disrupted nights.
- **Haghayegh et al. 2019**, *J Med Internet Res* 21:e16273 (Fitbit meta-analysis):
  sleep-staging models specificity 0.58-0.69; "not a substitute for PSG".
- **de Zambotti et al. 2019**, *Med Sci Sports Exerc* 51:1538-1557 and
  **Menghini et al. 2021**, *Sleep* 44(2):zsaa170: reviews and a standard
  validation framework (epoch-by-epoch, Bland-Altman). Used for how to report, not
  for numbers.

**What an estimate means.** Even trained, commercial wrist stagers agree with PSG on
roughly 50-70 % of 30-second epochs for four stages (kappa about 0.2-0.5), detect
sleep well and wake poorly. A hand-written rule set without PSG tuning should be
expected to do no better and probably worse.

## 3. What is reproducible with our data

| Method | Reproducible? | Why |
|---|---|---|
| van Hees 2015/2018 (angle) | Yes, already shipped | needs only gravity |
| Cole-Kripke, Sadeh, Webster | No | coefficients tied to actigraph count units we do not have |
| Walch 2019, Beattie 2017, Fonseca 2017, Radha 2019, Altini 2021 | No | trained on PSG, weights not published |
| Kortelainen 2010 HMM | No | emission/transition parameters trained on PSG |
| Transparent rules using published directions of change | Yes | what vitals ships, labelled as ours |

## 4. The method shipped (`internal/science/stages.go`)

30-second epochs from 30 min before to 30 min after the van Hees 2018 sleep period.
Per epoch: mean HR, mean movement value, arm-angle range, RR coverage, majority strap
state. Over a centred 5.5-min window of **still** epochs: SD of the 1 Hz heart rate
and RMSSD of clean, adjacent RR pairs (300-2000 ms, successive change <= 20 %;
at least 60 pairs, otherwise "insufficient RR"). Heart rate and log-RMSSD are
detrended with a Theil-Sen line across the sleep period (Trinder 2001's time effect).
Breathing rate from RR is computed but **not** used for staging, so it can serve as
an independent check.

1. **Wake**: outside the sleep period, or "active" (movement > 4x the night's median
   or arm angle swinging > 10 deg) with heart rate >= 4 bpm above trend, or active
   for >= 1.5 min in a row.
2. **Sleep onset**: first 10 min of unbroken sleep. REM is not allowed in the first
   30 min after it.
3. Robust per-night z-scores (median/MAD, clipped at +/-3; Fonseca 2015's idea).
   Deep score = mean(-z HR-above-trend, -z HR SD). REM score = mean(+z HR-above-trend,
   +z HR SD, -z detrended log-RMSSD). Optional time prior (default 0.25 SD) favours
   deep early and REM late (Dijk 2009). Scores smoothed over 2.5 min.
4. Deep or REM only when the score reaches 0.6 and the epoch is still; otherwise
   **light** (conservative default). Light gaps <= 3 min inside REM are REM; deep or
   REM bouts shorter than 5 min become light.
5. Confidence per epoch is a heuristic margin (0.5-0.85, x0.85 without RMSSD), not a
   probability; epochs under 0.6 are flagged low and drawn faded.
6. Cycles end at each REM period (REM bouts < 20 min apart merged).

Every number above is ours and unvalidated; the comments in the code say so.

## 5. Real night 2026-10-21/22 (user's own strap)

Default options (time prior 0.25):
- Rest onset 23:28 (van Hees window), sleep onset 23:29, final wake 06:10.
- TST 376 min: light 285 (76 %), deep 44.5 (12 %), REM 46.5 (12 %); wake in the
  scored span 87 min; WASO 25.5 min; REM latency 260 min; 2 cycles (277 and 122 min).
- Mean HR: deep 61.3, REM 62.1, light 63.3, wake 74.0 bpm. RR coverage in the sleep
  period 0.89; RMSSD available for all sleep epochs. Mean confidence 0.63.
- Strap's own sleep/wake label: 91 % epoch agreement, kappa 0.69.

With the time prior off: deep 43 min, REM 20 min (5 %), same cycles.

| Sanity check | Result | Independent of the rules? |
|---|---|---|
| Deep concentrated in the first third | **FAIL** (10 min first third, 0 last third, but most deep at 03:00-03:35, the middle third) | no with prior, yes without; fails both ways |
| REM more in the last third | pass (0 vs 40 min) | partly (prior) |
| REM periods lengthen | pass (16 -> 31 min) with prior; fail (12 -> 8) without | yes |
| HR lowest in deep | pass (61.3 < 62.1 < 63.3) | no: HR is an input |
| HR more variable in REM | pass | no: HR SD is an input |
| Breathing more variable in REM | pass (SD 5.0 vs 4.7 /min) but the RR-derived rate was LOWER in "REM" (11.7 vs 14.0 /min), which suggests artefact rather than REM breathing | yes |
| ~90-min cycles | **FAIL** (277 + 122 min: no REM was found in the first 4 h) | yes |
| REM share plausible (10-30 %) | pass at 12 % with prior; **FAIL** at 5 % without | yes |
| Agrees with strap's sleep/wake | pass (91 %, kappa 0.69) | yes |

Reading: sleep/wake is credible (it agrees with the strap's own label). The stage
split is not: the method found almost no REM in the first half of the night and no
clear deep sleep in the first cycle, which is not the typical architecture of a
27-year-old. Either the heart-rate features of this strap at 1 Hz do not separate the
stages well, or the rules are mis-set; without PSG there is no way to tell. That is
why the dashboard labels it "Estimate, confidence low" and lists the failed checks.

## 6. Tests
`internal/science/stages_test.go`: a synthetic five-cycle night (94 % epoch
agreement; this only shows the code does what the rules say, because the
synthetic data was built from the same assumptions), no RR at all (falls back to
heart rate, confidence "very low", caveat), a 1-hour off-wrist gap (scored as no
data), a 1-hour nap (very low confidence), a 10-minute window and no samples
(refused), a restless all-awake span (refused), featureless sleep (no invented
deep/REM), and a Theil-Sen outlier test. `internal/ingest/realnight_stages_test.go`
prints the real night when pointed at a copy of the database.

## 7. What would make this better (needs the user's decision)
- A reference: one night with a validated device (a home EEG headband or a lab
  PSG) would allow measuring agreement, and tuning the thresholds honestly.
- Raw optical data (ENABLE_OPTICAL_DATA / TOGGLE_OPTICAL_MODE) could give full beat
  detection and possibly a second channel; not enabled, needs permission and costs
  battery. The current 25 Hz bursts cover only 3 % of the night.
