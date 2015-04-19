var GroupsController = (function() {
	var groupsSecrets = {};
	var charts = {};

	var my = {
		getGroupsInfo: function(successCallback, errorCallback) {
			$.ajax({
				type: 'POST',
				url: cAPIBase + '/get_groups_by_user',
				data: {
					u: LoginController.getUser(),
					uk: LoginController.getKey(),
				},
				success: successCallback,
				error: errorCallback,
				dataType: 'json'
			});
		},

		init: function() {
			this.getGroupsInfo(plotGroupInfo);
		}
	};

	console.log("Groups controller", LoginController.getUser(), LoginController.getKey());

	var getShardsInfo = function(groupId, callback) {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/info',
			data: {
				uid: LoginController.getUser(),
				key: groupsSecrets[groupId],
				group: groupId
			},
			success: callback,
			dataType: 'json'
		});
	}

	var roundTwoDec = function(n) {
		return Math.round(n * 100) / 100
	};

	var updateGroupKey = function(groupId) {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/generate_group_key',
			data: {
				u: LoginController.getUser(),
				uk: LoginController.getKey(),
				g: groupId,
				k: groupsSecrets[groupId]
			},
			success: function(newKey) {
				$("#group-" + groupId + "-key").text(newKey);
				groupsSecrets[groupId] = newKey;
			}
		});
	};

	var updateShardsInfo = function (groupId) {
		setInterval(function () {
			getShardsInfo(groupId, function (data) {
				var x = (new Date()).getTime()
				$.each(data, function(k, v) {
					var shardIdSanit = k.replace(/\./g, "-");
					var secVal = v.queries_by_sec[v.queries_by_sec.length-1];
					var perc = roundTwoDec(secVal / ~~$("#group-" + groupId + "-req-sec").text() * 100);
					if (perc > 100) {
						perc = 100;
					}
					$("#shard-req-sec-" + groupId + "-" + shardIdSanit).text(secVal);
					var barDiv = $("#shard-req-sec-prog-bar-" + groupId + "-" + shardIdSanit);
					barDiv.css("width", perc + "%");
					barDiv.text(perc + "%");

					$("#shard-elems-stored-" + groupId + "-" + shardIdSanit).text(v.stored_elements);
					var percElems = roundTwoDec(v.stored_elements / ~~$("#group-" + groupId + "-max-elements").text() * 100);
					var barDivElems = $("#shard-elems-stored-prog-bar-" + groupId + "-" + shardIdSanit);
					barDivElems.css("width", percElems + "%");
					barDivElems.text(percElems + "%");

					$("#shard-status-" + groupId + "-" + shardIdSanit).text(v.rec_tree_status);

					if (perc > 80) {
						barDiv.addClass("progress-bar-danger");
						barDiv.removeClass("progress-bar-warning");
					} else if (perc > 65) {
						barDiv.addClass("progress-bar-warning");
						barDiv.removeClass("progress-bar-danger");
					} else {
						barDiv.removeClass("progress-bar-danger");
						barDiv.removeClass("progress-bar-warning");
					}

					charts[groupId + "-sec-" + shardIdSanit].addPoint([x, secVal], true, true);
					if (Math.round(x/1000) % 60 === 0) {
						var minVal = v.queries_by_min[v.queries_by_min.length-1];
						charts[groupId + "-min-" + shardIdSanit].addPoint([x, minVal], true, true);
					}
				});
			});
		}, 1000);
	};

	var addAnimatedChar = function(title, targetDiv, id, initialInfo, timeMult) {
		targetDiv.highcharts({
			chart: {
				type: 'spline',
				animation: Highcharts.svg,
				marginRight: 10,
				events: {
					load: function () {
						charts[id] = this.series[0];
					}
				}
			},
			title: {
				text: null,
			},
			xAxis: {
				type: 'datetime',
				tickPixelInterval: 150
			},
			yAxis: {
				title: {
					text: null,
				},
				plotLines: [{
					value: 0,
					width: 1,
					color: '#808080'
				}]
			},
			tooltip: {
				formatter: function () {
					return '<b>' + this.series.name + '</b><br/>' +
						Highcharts.dateFormat('%Y-%m-%d %H:%M:%S', this.x) + '<br/>' +
						Highcharts.numberFormat(this.y, 2);
				}
			},
			legend: {
				enabled: false
			},
			exporting: {
				enabled: false
			},
			series: [{
				name: title,
				data: (function () {
					var data = [],
					time = (new Date()).getTime(),
					i;

					for (i = 0; i < initialInfo.length; i++) {
						data.push({
							x: time - (initialInfo.length - (i * timeMult)) * 1000,
							y: initialInfo[i],
						});
					}
					return data;
				}())
			}]
		});
	}

	var removeShardsContent = function(groupId) {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/remove_group_shards_content',
			data: {
				u: LoginController.getUser(),
				uk: LoginController.getKey(),
				k: groupsSecrets[groupId],
				g: groupId,
			},
			success: function(newKey) {
				alert("Content removed, this action can take some time to have effect, please be pattient");
			}
		});
	};

	var updateShardsGroup = function(groupId, numShards) {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/set_shards_group',
			data: {
				u: LoginController.getUser(),
				uk: LoginController.getKey(),
				g: groupId,
				k: groupsSecrets[groupId],
				s: numShards
			},
			success: function(newKey) {
				alert("Updated");
			}
		});
	};

	var plotGroupInfo = function(data) {
		Highcharts.setOptions({
			global: {
				useUTC: false
			}
		});
		var groupsTemplate = Handlebars.compile($("#groups-template").html());
		var shardsTemplate = Handlebars.compile($("#shards-template").html());
		var containerDiv = $("#shads-container");

		containerDiv.html('');

		$.each(data, function (k, v) {
			var html = groupsTemplate(v);

			groupsSecrets[k] = v.secret;
			containerDiv.append(html);

			$("#shards-update-buttn-" + k).click(function() {
				updateShardsGroup(k, ~~$("#shards-update-txt-" + k).val());
			});

			$("#group-button-" + k + "-remove-all").click(function() {
				if (confirm('This action will remove all the content from the shards and stored backups, leaving them empty, this action can\'t be undone. Are you completly sure that you want to perform this action?')) {
					removeShardsContent(k);
				}
			});

			$("#group-button-" + k + "-key").click(function() {
				if (confirm('Are you sure that you want to regenerate the key for this group?, remember change it on all the clients')) {
					updateGroupKey(k);
				}
			});

			getShardsInfo(k, function(shardsData) {
				console.log(shardsData);
				var shardsGroupContainer = $("#group-shards-" + k);
				$.each(shardsData, function(host, shardInfo) {
					var statusLevel;

					switch(shardInfo.rec_tree_status) {
						case "STARTING":
							statusLevel = "primary";
							break;
						case "LOADING":
							statusLevel = "info";
							break;
						case "ACTIVE":
							statusLevel = "success";
							break;
						case "NO_RECORDS":
							statusLevel = "warning";
							break;
					}

					var reqsSec = shardInfo.queries_by_sec[shardInfo.queries_by_sec.length-1];
					var hostSanit = host.replace(/\./g, "-");
					var templateInfo = {
						group_id: k,
						shard_id_full: host,
						shard_id: hostSanit,
						elems_stored: shardInfo.stored_elements,
						reqs_sec: reqsSec,
						status_level: statusLevel,
						perc_stored: roundTwoDec((shardInfo.stored_elements / v.max_elems) * 100),
						perc_reqs: roundTwoDec((reqsSec / v.max_req_sec) * 100),
						shard_status: shardInfo.rec_tree_status
					};
					shardsGroupContainer.append(shardsTemplate(templateInfo));

					console.log(host, hostSanit);
					// Add the animated chart at the bottom
					addAnimatedChar("Req/sec", $("#req-sec-stats-" + k + "-" + hostSanit), k + "-sec-" + hostSanit, shardInfo.queries_by_sec, 1);
					addAnimatedChar("Req/min", $("#req-min-stats-" + k + "-" + hostSanit), k + "-min-" + hostSanit, shardInfo.queries_by_min, 60);
				});
			});

			updateShardsInfo(k);
		});
	};

	return my;
})();
