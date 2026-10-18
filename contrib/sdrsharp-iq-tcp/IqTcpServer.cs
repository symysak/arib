using System;
using System.Collections.Generic;
using System.Globalization;
using System.Net;
using System.Net.Sockets;
using System.Text;
using System.Threading;

namespace SDRSharp.IqTcpServer
{
    public sealed class IqTcpServer
    {
        private const int QueueCapacity = 64;

        internal const byte FrameIq = 0x01;
        internal const byte FrameTuning = 0x02;

        private readonly object _sync = new object();
        private readonly List<Client> _clients = new List<Client>();
        private TcpListener _listener;
        private Thread _acceptThread;
        private volatile bool _running;
        private byte[] _tuningFrame;
        private string _tuningJson;

        public int Port { get; private set; }
        public bool Running => _running;

        public int ClientCount
        {
            get { lock (_sync) return _clients.Count; }
        }

        public event Action Changed;

        public void Start(int port)
        {
            Stop();
            _listener = new TcpListener(IPAddress.Any, port);
            _listener.Start();
            Port = port;
            _running = true;
            _acceptThread = new Thread(AcceptLoop) { IsBackground = true, Name = "IqTcp-Accept" };
            _acceptThread.Start();
            RaiseChanged();
        }

        public void Stop()
        {
            if (!_running && _listener == null) return;
            _running = false;
            try { _listener?.Stop(); } catch {  }
            _listener = null;
            lock (_sync)
            {
                foreach (var c in _clients) c.Dispose();
                _clients.Clear();
            }
            RaiseChanged();
        }

        private void AcceptLoop()
        {
            while (_running)
            {
                Socket sock;
                try
                {
                    sock = _listener.AcceptSocket();
                }
                catch
                {
                    break;
                }
                try { sock.NoDelay = true; } catch {  }

                var client = new Client(sock, QueueCapacity, OnClientClosed);
                lock (_sync)
                {
                    _clients.Add(client);
                    if (_tuningFrame != null) client.Enqueue(_tuningFrame);
                }
                client.Start();
                RaiseChanged();
            }
        }

        private void OnClientClosed(Client c)
        {
            bool removed;
            lock (_sync) removed = _clients.Remove(c);
            c.Dispose();
            if (removed) RaiseChanged();
        }

        public void PublishTuning(double centerHz, double vfoHz, double rate)
        {
            if (!(rate > 0.0)) return;
            string json = string.Format(CultureInfo.InvariantCulture,
                "{{\"center\":{0:R},\"vfo\":{1:R},\"rate\":{2:R}}}", centerHz, vfoHz, rate);
            lock (_sync)
            {
                if (json == _tuningJson) return;
                _tuningJson = json;
                _tuningFrame = MakeFrame(FrameTuning, Encoding.UTF8.GetBytes(json));
                for (int i = 0; i < _clients.Count; i++)
                    _clients[i].Enqueue(_tuningFrame);
            }
        }

        public void Broadcast(byte[] iq)
        {
            if (iq == null || iq.Length == 0) return;
            byte[] frame = null;
            lock (_sync)
            {
                for (int i = 0; i < _clients.Count; i++)
                {
                    var c = _clients[i];
                    if (!c.HasTuning) continue;
                    if (frame == null) frame = MakeFrame(FrameIq, iq);
                    c.Enqueue(frame);
                }
            }
        }

        internal static byte[] MakeFrame(byte type, byte[] body)
        {
            var f = new byte[5 + body.Length];
            f[0] = type;
            uint n = (uint)body.Length;
            f[1] = (byte)n;
            f[2] = (byte)(n >> 8);
            f[3] = (byte)(n >> 16);
            f[4] = (byte)(n >> 24);
            Buffer.BlockCopy(body, 0, f, 5, body.Length);
            return f;
        }

        private void RaiseChanged()
        {
            var h = Changed;
            if (h != null) h();
        }


        private sealed class Client
        {
            private readonly Socket _sock;
            private readonly LinkedList<byte[]> _queue = new LinkedList<byte[]>();
            private readonly int _capacity;
            private readonly Thread _sender;
            private readonly Action<Client> _onClosed;
            private volatile bool _alive = true;
            private volatile bool _hasTuning;

            public Client(Socket sock, int capacity, Action<Client> onClosed)
            {
                _sock = sock;
                _capacity = capacity;
                _onClosed = onClosed;
                _sender = new Thread(SendLoop) { IsBackground = true, Name = "IqTcp-Send" };
            }

            public bool HasTuning => _hasTuning;

            public void Start() => _sender.Start();

            public void Enqueue(byte[] frame)
            {
                if (!_alive) return;
                lock (_queue)
                {
                    if (frame[0] == FrameTuning) _hasTuning = true;
                    while (_queue.Count >= _capacity)
                    {
                        var node = _queue.First;
                        while (node != null && node.Value[0] != FrameIq) node = node.Next;
                        if (node == null) break;
                        _queue.Remove(node);
                    }
                    _queue.AddLast(frame);
                    Monitor.Pulse(_queue);
                }
            }

            private byte[] Dequeue()
            {
                lock (_queue)
                {
                    while (_alive && _queue.Count == 0) Monitor.Wait(_queue);
                    if (!_alive) return null;
                    var f = _queue.First.Value;
                    _queue.RemoveFirst();
                    return f;
                }
            }

            private void SendLoop()
            {
                try
                {
                    while (_alive)
                    {
                        byte[] frame = Dequeue();
                        if (frame == null) break;
                        int off = 0;
                        while (off < frame.Length)
                        {
                            int sent = _sock.Send(frame, off, frame.Length - off, SocketFlags.None);
                            if (sent <= 0) throw new SocketException((int)SocketError.ConnectionReset);
                            off += sent;
                        }
                    }
                }
                catch
                {
                }
                finally
                {
                    _onClosed?.Invoke(this);
                }
            }

            public void Dispose()
            {
                if (!_alive) return;
                _alive = false;
                lock (_queue) Monitor.PulseAll(_queue);
                try { _sock.Shutdown(SocketShutdown.Both); } catch {  }
                try { _sock.Close(); } catch {  }
            }
        }
    }
}
