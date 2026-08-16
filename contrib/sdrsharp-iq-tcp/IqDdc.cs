using System;
using SDRSharp.Radio;

namespace SDRSharp.IqTcpServer
{
    internal sealed unsafe class IqDdc : IDisposable
    {
        public const double MinOutputRate = 40_000.0;

        private const int NormalizeInterval = 512;

        private readonly object _sync = new object();

        private double _sampleRate;
        private bool _followVfo = true;
        private bool _decimate = true;
        private double _offsetHz;

        private ComplexDecimator _decimator;
        private int _ratio = 1;
        private Complex[] _scratch = new Complex[0];

        private double _phaseRe = 1.0, _phaseIm;
        private double _stepRe = 1.0, _stepIm;
        private int _sinceNormalize;

        private double _powerSum;
        private long _powerCount;
        private long _outputSamples;

        public double SampleRate
        {
            get { lock (_sync) return _sampleRate; }
            set
            {
                lock (_sync)
                {
                    if (_sampleRate == value) return;
                    _sampleRate = value;
                    Rebuild();
                }
            }
        }

        public bool FollowVfo
        {
            get { lock (_sync) return _followVfo; }
            set
            {
                lock (_sync)
                {
                    if (_followVfo == value) return;
                    _followVfo = value;
                    Rebuild();
                }
            }
        }

        public bool Decimate
        {
            get { lock (_sync) return _decimate; }
            set
            {
                lock (_sync)
                {
                    if (_decimate == value) return;
                    _decimate = value;
                    Rebuild();
                }
            }
        }

        public double OffsetHz
        {
            get { lock (_sync) return _offsetHz; }
            set
            {
                lock (_sync)
                {
                    if (_offsetHz == value) return;
                    _offsetHz = value;
                    UpdateStep();
                }
            }
        }

        public double OutputSampleRate
        {
            get { lock (_sync) return _ratio > 1 ? _sampleRate / _ratio : _sampleRate; }
        }

        public int DecimationRatio
        {
            get { lock (_sync) return _ratio; }
        }

        public long OutputSampleCount
        {
            get { lock (_sync) return _outputSamples; }
        }

        public double TakeLevelDbfs()
        {
            lock (_sync)
            {
                if (_powerCount <= 0) return double.NaN;
                double mean = _powerSum / _powerCount;
                _powerSum = 0.0;
                _powerCount = 0;
                return mean > 0.0 ? 10.0 * Math.Log10(mean) : double.NegativeInfinity;
            }
        }

        public byte[] Process(Complex* src, int length)
        {
            if (length <= 0) return null;
            lock (_sync)
            {
                if (!_followVfo)
                {
                    AccumulateOutput(src, length);
                    var raw = new byte[length * 8];
                    fixed (byte* dst = raw)
                    {
                        Buffer.MemoryCopy(src, dst, raw.Length, raw.Length);
                    }
                    return raw;
                }

                if (_scratch.Length < length) _scratch = new Complex[length];
                fixed (Complex* buf = _scratch)
                {
                    long nbytes = (long)length * 8;
                    Buffer.MemoryCopy(src, buf, nbytes, nbytes);
                    Mix(buf, length);
                    int n = _decimator != null ? _decimator.Process(buf, length) : length;
                    if (n <= 0) return null;
                    AccumulateOutput(buf, n);
                    var block = new byte[n * 8];
                    fixed (byte* dst = block)
                    {
                        Buffer.MemoryCopy(buf, dst, block.Length, block.Length);
                    }
                    return block;
                }
            }
        }

        public void Dispose()
        {
            lock (_sync)
            {
                if (_decimator != null)
                {
                    _decimator.Dispose();
                    _decimator = null;
                }
                _ratio = 1;
            }
        }


        private void Mix(Complex* buf, int length)
        {
            double pr = _phaseRe, pi = _phaseIm;
            double sr = _stepRe, si = _stepIm;
            for (int i = 0; i < length; i++)
            {
                double xr = buf[i].Real, xi = buf[i].Imag;
                buf[i].Real = (float)(xr * pr - xi * pi);
                buf[i].Imag = (float)(xr * pi + xi * pr);
                double nr = pr * sr - pi * si;
                pi = pr * si + pi * sr;
                pr = nr;
                if (++_sinceNormalize >= NormalizeInterval)
                {
                    _sinceNormalize = 0;
                    double m = Math.Sqrt(pr * pr + pi * pi);
                    if (m > 0.0) { pr /= m; pi /= m; }
                }
            }
            _phaseRe = pr;
            _phaseIm = pi;
        }

        private void AccumulateOutput(Complex* buf, int length)
        {
            double sum = 0.0;
            for (int i = 0; i < length; i++)
            {
                double re = buf[i].Real, im = buf[i].Imag;
                sum += re * re + im * im;
            }
            _powerSum += sum;
            _powerCount += length;
            _outputSamples += length;
        }

        private void Rebuild()
        {
            if (_decimator != null)
            {
                _decimator.Dispose();
                _decimator = null;
            }
            _ratio = 1;
            if (_followVfo && _decimate && _sampleRate >= 2.0 * MinOutputRate)
            {
                for (int arg = 1; arg <= 8; arg++)
                {
                    ComplexDecimator candidate;
                    try
                    {
                        candidate = new ComplexDecimator(arg);
                    }
                    catch
                    {
                        continue;
                    }
                    int ratio = candidate.DecimationRatio;
                    if (ratio > _ratio && _sampleRate / ratio >= MinOutputRate)
                    {
                        if (_decimator != null) _decimator.Dispose();
                        _decimator = candidate;
                        _ratio = ratio;
                    }
                    else
                    {
                        candidate.Dispose();
                    }
                }
            }
            UpdateStep();
            _powerSum = 0.0;
            _powerCount = 0;
        }

        private void UpdateStep()
        {
            double w = _sampleRate > 0.0 ? -2.0 * Math.PI * _offsetHz / _sampleRate : 0.0;
            _stepRe = Math.Cos(w);
            _stepIm = Math.Sin(w);
        }
    }
}
