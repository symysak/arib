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
        private double _sampleRate;

        public string DisplayName => "IQ TCP Server (cf32)";
        public bool HasGui => true;
        public UserControl Gui => _panel;

        internal IqTcpServer Server => _server;

        public void Initialize(ISharpControl control)
        {
            _control = control;
            _panel = new IqTcpServerPanel(this);
            control.RegisterStreamHook(this, ProcessorType.RawIQ);
        }

        public void Close()
        {
            _server.Stop();
        }


        public bool Enabled { get; set; }

        public double SampleRate
        {
            get { return _sampleRate; }
            set
            {
                _sampleRate = value;
                if (_panel != null) _panel.SetSampleRate(value);
            }
        }

        public void Process(Complex* buffer, int length)
        {
            if (!Enabled || length <= 0) return;

            int nbytes = length * 8;
            var block = new byte[nbytes];
            fixed (byte* dst = block)
            {
                Buffer.MemoryCopy(buffer, dst, nbytes, nbytes);
            }
            _server.Broadcast(block);
        }
    }
}
