using System;
using System.Windows.Forms;
using SDRSharp.Common;
using SDRSharp.Radio;

namespace SDRSharp.IqTcpServer
{
    public unsafe class IqTcpServerPlugin : ISharpPlugin, IIQProcessor
    {
        private ISharpControl _control;
        private IqTcpServerPanel _panel;
        private readonly IqTcpServer _server = new IqTcpServer();
        private readonly IqDdc _ddc = new IqDdc();
        private double _sampleRate;

        public string DisplayName => "IQ TCP Server (cf32)";
        public bool HasGui => true;
        public UserControl Gui => _panel;

        internal IqTcpServer Server => _server;
        internal IqDdc Ddc => _ddc;

        public void Initialize(ISharpControl control)
        {
            _control = control;
            _panel = new IqTcpServerPanel(this);
            control.RegisterStreamHook(this, ProcessorType.RawIQ);
        }

        public void Close()
        {
            _server.Stop();
            _ddc.Dispose();
        }

        internal double VfoOffsetHz
        {
            get
            {
                if (_control == null) return 0.0;
                try
                {
                    return (double)(_control.Frequency - _control.CenterFrequency);
                }
                catch
                {
                    return 0.0;
                }
            }
        }

        internal double SyncVfoOffset()
        {
            double offset = VfoOffsetHz;
            _ddc.OffsetHz = _ddc.FollowVfo ? offset : 0.0;
            return offset;
        }


        public bool Enabled { get; set; }

        public double SampleRate
        {
            get { return _sampleRate; }
            set
            {
                _sampleRate = value;
                _ddc.SampleRate = value;
                if (_panel != null) _panel.SetSampleRate(value);
            }
        }

        public void Process(Complex* buffer, int length)
        {
            if (!Enabled || length <= 0) return;

            var block = _ddc.Process(buffer, length);
            if (block != null && block.Length > 0) _server.Broadcast(block);
        }
    }
}
